package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/hollow-cube/hc-services/libraries/common/pkg/common"
	"github.com/hollow-cube/hc-services/libraries/common/pkg/kafkafx"
	"github.com/hollow-cube/hc-services/services/session-service/internal/db"
	"github.com/hollow-cube/hc-services/services/session-service/internal/pkg/authz"
	"github.com/hollow-cube/hc-services/services/session-service/internal/pkg/model"
	"github.com/hollow-cube/hc-services/services/session-service/internal/pkg/player"
	"github.com/hollow-cube/hc-services/services/session-service/internal/pkg/text"
	"github.com/redis/rueidis"
	"github.com/segmentio/kafka-go"
	"go.uber.org/fx"
	"go.uber.org/zap"
	"k8s.io/client-go/kubernetes"
)

const (
	chatTopicId    = "chat"
	outChatTopicId = "chat-messages"
)

var (
	emojiRegex = regexp.MustCompile(`:([a-zA-Z0-9\-_]+):`)
	mapRegex   = regexp.MustCompile(`\[map]`)
	urlRegex   = regexp.MustCompile(`[(http(s)?):\/\/(www\.)?a-zA-Z0-9@:%._\+~#=]{2,256}\.[a-z]{2,6}\b([-a-zA-Z0-9@:%_\+.~#?&//=]*)`)
)

type ChatHandler struct {
	log *zap.SugaredLogger

	contentFilter text.Filter

	authzClient authz.Client
	queries     *db.Queries
	redis       rueidis.Client
	producer    kafkafx.SyncProducer

	playerTracker *player.Tracker
}

type ChatHandlerParams struct {
	fx.In

	Log *zap.SugaredLogger

	AuthzClient      authz.Client
	Queries          *db.Queries
	KubernetesClient *kubernetes.Clientset
	Redis            rueidis.Client
	PlayerTracker    *player.Tracker
	Consumer         kafkafx.Consumer
	Producer         kafkafx.SyncProducer
}

func NewChatHandler(p ChatHandlerParams) (*ChatHandler, error) {
	//hostname, _ := os.Hostname()

	handler := &ChatHandler{
		log: p.Log.With("handler", "chat"),

		contentFilter: text.NewStaticFilter(),

		authzClient: p.AuthzClient,
		queries:     p.Queries,
		redis:       p.Redis,
		producer:    p.Producer,

		playerTracker: p.PlayerTracker,
	}

	p.Consumer.Subscribe(chatTopicId, "session-service-chat", handler.handleConsumerMessage, kafkafx.WithIsolationLevel(kafka.ReadCommitted))

	return handler, nil
}

func (h *ChatHandler) HandleUnsignedChatMessage(ctx context.Context, msg *model.ClientChatMessage) error {
	// Sanitize the message for invalid characters
	text := text.StripSpecial(msg.Message)
	if len(text) == 0 {
		return nil
	}

	channel, ok := h.resolveMessageChannel(ctx, msg)
	if !ok {
		return nil // Error system message was already sent
	}

	censor := h.contentFilter.Test(ctx, text)

	// Record the chat message (async), even if censored
	go func() {
		var censoredBy, censoredDetail *string
		if censor.Matched {
			censoredBy = &censor.Engine
			censoredDetail = &censor.MatchedText
		}
		if err := h.queries.InsertChatMessage(ctx, db.InsertChatMessageParams{
			Timestamp:      time.Now(),
			ServerID:       "unknown",
			Channel:        string(channel),
			Sender:         msg.Sender,
			Content:        msg.Message,
			CensoredBy:     censoredBy,
			CensoredDetail: censoredDetail,
		}); err != nil {
			h.log.Errorw("failed to record chat message", "error", err)
		}
	}()

	if censor.Matched {
		// Reply to the player indicating that the message was censored
		h.sendMessageToServer(ctx, &model.ChatMessage{
			Type:   model.ChatSystem,
			Target: msg.Sender,
			Key:    "chat.censored",
		})
		return nil
	}

	parts := []model.MessagePart{&model.RawMessagePart{Text: text}}

	// Replace urls
	parts = regexReplaceInMessage(parts, urlRegex, func(match string) model.MessagePart {
		return &model.UrlMessagePart{
			Type: model.PartTypeUrl,
			Text: match,
		}
	})

	// Replace emojis with their names
	parts = regexReplaceInMessage(parts, emojiRegex, func(match string) model.MessagePart {
		return &model.EmojiMessagePart{
			Type: model.PartTypeEmoji,
			Name: match[1 : len(match)-1],
		}
	})

	// Replace [map]
	parts = regexReplaceInMessage(parts, mapRegex, func(match string) model.MessagePart {
		return &model.MapMessagePart{
			Type:  model.PartTypeMap,
			MapID: msg.CurrentMap,
		}
	})

	hasHyperCube, err := h.authzClient.HasHypercube(ctx, msg.Sender, authz.NoKey)

	if err != nil {
		return fmt.Errorf("could not filter emojis: %w", err)
	}

	h.sendMessageToServer(ctx, &model.ChatMessage{
		Type:               model.ChatUnsigned,
		Channel:            channel,
		Sender:             msg.Sender,
		Parts:              parts,
		Seed:               msg.Seed,
		SenderHasHypercube: hasHyperCube,
	})

	// If this is a DM, update the reply channels for both sides
	if common.IsUUID(string(msg.Channel)) {
		h.updateLastMessageChannel(ctx, msg.Sender, msg.Channel)
		h.updateLastMessageChannel(ctx, string(msg.Channel), model.ChatChannel(msg.Sender))
	}

	return nil
}

func (h *ChatHandler) resolveMessageChannel(ctx context.Context, msg *model.ClientChatMessage) (model.ChatChannel, bool) {
	channel := msg.Channel

	// If sending a reply, lookup the player's last dm target
	if channel == model.ChannelReply {
		lastTarget, err := h.redis.Do(ctx, h.redis.B().Get().Key(fmt.Sprintf("sess:player:%s:reply", msg.Sender)).Build()).ToString()
		if errors.Is(err, rueidis.Nil) {
			h.sendMessageToServer(ctx, &model.ChatMessage{
				Type:   model.ChatSystem,
				Target: msg.Sender,
				Key:    "chat.reply.no_target",
			})
			return "", false
		}
		if err != nil {
			h.log.Errorw("failed to fetch last reply target", "error", err)
			return "", false
		}

		// Ensure they are still online
		if s, _ := h.playerTracker.GetSession(ctx, lastTarget); s == nil {
			h.sendMessageToServer(ctx, &model.ChatMessage{
				Type:   model.ChatSystem,
				Target: msg.Sender,
				Key:    "generic.player.offline",
			})
		}

		channel = model.ChatChannel(lastTarget)
	}

	return channel, true
}

func (h *ChatHandler) updateLastMessageChannel(ctx context.Context, player string, channel model.ChatChannel) {
	//todo this is giga cursed because we remove the key in the player manager... need to rework a lot of this
	err := h.redis.Do(ctx, h.redis.B().Set().Key(fmt.Sprintf("sess:player:%s:reply", player)).Value(string(channel)).Build()).Error()
	if err != nil {
		h.log.Errorw("failed to update last reply target", "error", err)
	}
}

func (h *ChatHandler) sendMessageToServer(ctx context.Context, msg *model.ChatMessage) {
	raw, err := json.Marshal(msg)
	if err != nil {
		h.log.Errorw("failed to marshal chat message", "error", err)
		return
	}

	if err = h.producer.WriteMessages(ctx, kafka.Message{Topic: outChatTopicId, Value: raw}); err != nil {
		h.log.Errorw("failed to write chat message", "error", err)
		return
	}
}

func regexReplaceInMessage(current []model.MessagePart, expr *regexp.Regexp, f func(match string) model.MessagePart) []model.MessagePart {
	return mapMessageParts(current, func(part model.MessagePart) []model.MessagePart {
		switch part := part.(type) {
		case *model.RawMessagePart:
			loc := expr.FindStringIndex(part.Text)
			if loc == nil {
				return nil
			}

			var newParts []model.MessagePart
			// Append the preceding text as a new raw part, if it exists
			if loc[0] > 0 {
				newParts = append(newParts, &model.RawMessagePart{Type: model.PartTypeRaw, Text: part.Text[:loc[0]]})
			}

			// Append the matching part transformed by function f
			newParts = append(newParts, f(part.Text[loc[0]:loc[1]]))

			// Append the remaining text, recursively processed, if it exists
			if loc[1] < len(part.Text) {
				textPart := &model.RawMessagePart{Type: model.PartTypeRaw, Text: part.Text[loc[1]:]}
				newParts = append(newParts, regexReplaceInMessage([]model.MessagePart{textPart}, expr, f)...)
			}

			return newParts

		default:
			return nil
		}
	})
}

func mapMessageParts(current []model.MessagePart, mapper func(part model.MessagePart) []model.MessagePart) []model.MessagePart {
	var result []model.MessagePart
	for _, part := range current {
		pieces := mapper(part)
		if pieces == nil {
			result = append(result, part)
		} else {
			result = append(result, pieces...)
		}
	}
	return result
}

func (h *ChatHandler) handleConsumerMessage(ctx context.Context, m kafka.Message) error {
	h.log.Infow("new message", "offset", m.Offset, "lag", m.HighWaterMark-m.Offset-1)

	// Parse the message
	var msg model.ClientChatMessage
	if err := json.Unmarshal(m.Value, &msg); err != nil {
		return fmt.Errorf("failed to unmarshal chat message: %w", err)
	}

	// Handle the message
	switch msg.Type {
	case model.ChatUnsigned:
		if err := h.HandleUnsignedChatMessage(ctx, &msg); err != nil {
			return fmt.Errorf("failed to handle unsigned chat message: %w", err)
		}
	default:
		h.log.Errorw("unknown message type", "type", msg.Type)
	}

	return nil
}
