package discord

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/hollow-cube/api-server/config"
	"github.com/hollow-cube/api-server/internal/pkg/tracefx"
	"github.com/hollow-cube/api-server/internal/playerdb"
	"go.uber.org/fx"
	"go.uber.org/zap"
)

type Command struct {
	discordgo.ApplicationCommand

	// If true, the handler immediately respond with a deferred interaction, allowing the handler to
	// have >3 seconds to respond. If false, a context will be timed out after 3 seconds.
	deferred bool
	handler  func(h *Handler, ctx context.Context, i *discordgo.Interaction) (*discordgo.InteractionResponse, error)
}

var globalCommands = []*Command{
	&linkCommand,
}

// TODO: should register only to the main discord server, not globally.
var mainDiscordCommands = []*Command{
	&syncRolesCommand,
}

type HandlerParams struct {
	fx.In

	Log    *zap.SugaredLogger
	Config *config.Config

	Discord *discordgo.Session `optional:"true"`
	Store   *playerdb.Store
}

type Handler struct {
	log     *zap.SugaredLogger
	discord *discordgo.Session
	store   *playerdb.Store

	publicKey ed25519.PublicKey
	commands  map[string]*Command
}

func NewHandler(p HandlerParams) (*Handler, error) {
	if p.Discord == nil {
		p.Log.Info("Discord session not provided, not handling commands")
		return nil, nil
	}

	appID := p.Config.Discord.ApplicationID

	publicKey, err := hex.DecodeString(p.Config.Discord.PublicKey)
	if err != nil {
		panic(err)
	}

	var commands = make(map[string]*Command)
	var toRegister []*discordgo.ApplicationCommand
	for _, cmd := range globalCommands {
		commands[cmd.Name] = cmd
		toRegister = append(toRegister, &cmd.ApplicationCommand)
	}

	registered, err := p.Discord.ApplicationCommandBulkOverwrite(appID, "", toRegister)
	if err != nil {
		return nil, fmt.Errorf("failed to sync application commands: %w", err)
	}
	p.Log.Infow("Synced application commands", "count", len(registered), "application_id", appID, "commands", registered)

	return &Handler{
		log:       p.Log,
		discord:   p.Discord,
		store:     p.Store,
		publicKey: publicKey,
		commands:  commands,
	}, nil
}

func (h *Handler) OnDiscordWebhook(w http.ResponseWriter, r *http.Request) {
	if !discordgo.VerifyInteraction(r, h.publicKey) {
		http.Error(w, "invalid request signature", http.StatusUnauthorized)
		return
	}

	var interaction discordgo.Interaction
	if err := json.NewDecoder(r.Body).Decode(&interaction); err != nil {
		h.log.Errorw("failed to decode interaction", "err", err)
		http.Error(w, "could not read request body", http.StatusInternalServerError)
		return
	}

	resp, err := h.HandleInteraction(r.Context(), &interaction)
	if err != nil {
		h.log.Errorw("failed to handle interaction", "err", err)
		http.Error(w, "failed to handle interaction", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (h *Handler) HandleInteraction(ctx context.Context, i *discordgo.Interaction) (*discordgo.InteractionResponse, error) {
	switch i.Type {
	case discordgo.InteractionPing:
		return &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponsePong,
		}, nil
	case discordgo.InteractionApplicationCommand:
		cmd := h.commands[i.ApplicationCommandData().Name]
		if cmd == nil {
			return nil, fmt.Errorf("unknown command: %s", i.ApplicationCommandData().Name)
		}

		if cmd.deferred {
			// If deferred we need to make a new context, copy the tracing info, and send
			handleCtx := tracefx.NewCtxWithTraceCtx(ctx)

			go func() {
				resp, err := cmd.handler(h, handleCtx, i)
				if err != nil {
					h.log.Errorw("error handling deferred command", "command", cmd.Name, "error", err)
					return
				}

				if err = h.discord.InteractionRespond(i, resp); err != nil {
					h.log.Errorw("error responding to deferred command", "command", cmd.Name, "error", err)
					return
				}
			}()

			return &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
			}, nil
		}

		handleCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		return cmd.handler(h, handleCtx, i)
	//case discordgo.InteractionApplicationCommandAutocomplete:
	//	return &discordgo.InteractionResponse{
	//		Type: discordgo.InteractionApplicationCommandAutocompleteResult,
	//		Data: &discordgo.InteractionResponseData{
	//			Choices: []*discordgo.ApplicationCommandOptionChoice{
	//				{
	//					Name:  "Parkour Spiral by iTMG — 123-456-789",
	//					Value: "my-long-uuid",
	//				},
	//			},
	//		},
	//	}, nil

	//case discordgo.InteractionMessageComponent:
	//case discordgo.InteractionModalSubmit:
	default:
		return nil, fmt.Errorf("unsupported interaction type: %s", i.Type.String())
	}
}
