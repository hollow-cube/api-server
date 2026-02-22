//go:generate go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen -o server.gen.go -package payments -generate types,std-http-server openapi.yaml

// NOTE: We use the non-strict server here because we need to access the raw request body for the
// tebex webhook endpoint and the strict server does not provide a sane way of doing this. Additionally
// setting the expected type in the spec to a binary stream with application/json does not work.
//
// Accepting arbitrary json then decoding and encoding again also does not work because key order &
// whitespace changes which invalidates the signature check.

package payments

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/hollow-cube/hc-services/libraries/common/pkg/natsutil"
	"github.com/hollow-cube/hc-services/services/player-service/config"
	"github.com/hollow-cube/hc-services/services/player-service/internal/db"
	"github.com/hollow-cube/tebex-go"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/posthog/posthog-go"
	"go.uber.org/fx"
	"go.uber.org/zap"
)

var (
	tebexStream         = "TEBEX_PAYMENTS"
	tebexDlqStream      = "TEBEX_PAYMENTS_DLQ"
	tebexMaxDeliver     = 5
	tebexConsumerConfig = jetstream.ConsumerConfig{
		Name:          "tebex-processor",
		Durable:       "tebex-processor",
		FilterSubject: "tebex.>",
		AckPolicy:     jetstream.AckExplicitPolicy,
		AckWait:       30 * time.Second,
		MaxDeliver:    tebexMaxDeliver,
		DeliverPolicy: jetstream.DeliverAllPolicy,
		MaxAckPending: 50,
	}
)

var _ ServerInterface = (*server)(nil)

type ServerParams struct {
	fx.In

	Shutdowner fx.Shutdowner
	Lifecycle  fx.Lifecycle
	Log        *zap.SugaredLogger
	Config     *config.Config

	JetStream *natsutil.JetStreamWrapper
	Store     *db.Store
	Posthog   posthog.Client
}

func NewServer(lc fx.Lifecycle, params ServerParams) (ServerInterface, error) {
	tebexSecret := params.Config.Tebex.Secret
	if tebexSecret == "" {
		return nil, fmt.Errorf("tebex secret is required")
	}
	if tebexSecret == "explicit_ignore" {
		// Used in tilt to ignore the secret
		tebexSecret = ""
		params.Log.Info("tebex secret is explicitly ignored")
	}

	err := params.JetStream.UpsertStream(context.Background(), jetstream.StreamConfig{
		Name:       tebexStream,
		Subjects:   []string{"tebex.>"},
		Storage:    jetstream.FileStorage,
		Retention:  jetstream.WorkQueuePolicy,
		MaxAge:     30 * 24 * time.Hour,
		Duplicates: 5 * time.Minute,
		MaxMsgs:    -1,
		MaxBytes:   -1,
	})
	if err != nil {
		return nil, err
	}

	err = params.JetStream.UpsertStream(context.Background(), jetstream.StreamConfig{
		Name:      tebexDlqStream,
		Subjects:  []string{"tebex-dlq.>"},
		Storage:   jetstream.FileStorage,
		Retention: jetstream.LimitsPolicy,
		MaxAge:    365 * 24 * time.Hour,
		Discard:   jetstream.DiscardOld,
	})
	if err != nil {
		return nil, err
	}

	s := &server{
		log:         params.Log.With("handler", "payments"),
		tebexSecret: []byte(tebexSecret),
		jetStream:   params.JetStream,
		store:       params.Store,
		posthog:     params.Posthog,
	}

	cons, err := params.JetStream.Subscribe(context.Background(), tebexStream, tebexConsumerConfig, s.processStoredEventStream)
	if err != nil {
		return nil, fmt.Errorf("failed to subscribe to payment events: %w", err)
	}
	lc.Append(fx.StartStopHook(cons.Start, cons.Stop))

	return s, nil
}

type server struct {
	log         *zap.SugaredLogger
	tebexSecret []byte

	jetStream *natsutil.JetStreamWrapper
	store     *db.Store
	posthog   posthog.Client
}

func (s *server) OnTebexWebhook(w http.ResponseWriter, r *http.Request, params OnTebexWebhookParams) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		s.log.Errorw("failed to read request body", "err", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	// Do not pass the remote address, it is proxied so will be meaningless.
	err = tebex.ValidatePayloadRaw(params.ContentType, body, params.XSignature, s.tebexSecret, "")
	if err != nil {
		s.log.Errorw("failed to validate webhook payload", "err", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	// If this is a validation event, just return the ID to Tebex to acknowledge the webhook
	event, err := tebex.ParseEvent(body)
	if err != nil {
		s.log.Errorw("failed to parse webhook event", "err", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if event.Type == tebex.ValidationEventType {
		s.log.Infow("tebex webhook validation event received", "id", event.Id)
		err = json.NewEncoder(w).Encode(map[string]string{
			"id": event.Id,
		})
		if err != nil {
			s.log.Errorw("failed to encode validation response", "err", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		return
	}

	subject := fmt.Sprintf("tebex.%s", event.Type)
	header := nats.Header{
		"Nats-Msg-Id": []string{event.Id},
	}
	if err = s.jetStream.PublishAsyncWithHeader(r.Context(), subject, body, header); err != nil {
		s.log.Errorw("failed to write to kafka", "err", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	// OK, return happy response to Tebex
	w.WriteHeader(http.StatusOK)
	return
}

func (s *server) GetTebexBasket(w http.ResponseWriter, r *http.Request, params GetTebexBasketParams) {
	res, err := s.store.GetPendingTransactionByCheckoutId(r.Context(), params.Ref)
	if errors.Is(err, db.ErrNoRows) {
		w.WriteHeader(http.StatusNotFound)
		return
	} else if err != nil {
		s.log.Errorw("failed to get pending transaction", "err", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	basketId, username := res.BasketID, res.Username
	if basketId == nil {
		// Indicates that we have not created the basket yet and the client should refetch.
		w.WriteHeader(http.StatusNoContent)
		return
	} else if *basketId == "" {
		// Would happen if we failed to create the tebex basket. This is a terminal error for the client.
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	err = json.NewEncoder(w).Encode(map[string]string{
		"id":       *basketId,
		"username": username,
	})
	if err != nil {
		s.log.Errorw("failed to encode basket response", "err", err)
	}
	return
}
