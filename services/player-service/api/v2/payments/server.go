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

	"github.com/hollow-cube/hc-services/services/player-service/config"
	"github.com/hollow-cube/hc-services/services/player-service/internal/db"
	"github.com/hollow-cube/hc-services/services/player-service/internal/pkg/authz"
	"github.com/hollow-cube/hc-services/services/player-service/internal/pkg/payments"
	"github.com/hollow-cube/hc-services/services/player-service/internal/pkg/wkafka"
	"github.com/hollow-cube/tebex-go"
	"github.com/posthog/posthog-go"
	"github.com/segmentio/kafka-go"
	"go.uber.org/fx"
	"go.uber.org/zap"
)

var _ ServerInterface = (*server)(nil)

type ServerParams struct {
	fx.In

	Shutdowner fx.Shutdowner
	Lifecycle  fx.Lifecycle
	Log        *zap.SugaredLogger
	Config     *config.Config

	ReaderFactory wkafka.ReaderFactory
	Producer      wkafka.SyncWriter
	Store         *db.Store
	Authz         authz.Client
	Posthog       posthog.Client
}

func NewServer(params ServerParams) (ServerInterface, error) {
	tebexSecret := params.Config.Tebex.Secret
	if tebexSecret == "" {
		return nil, fmt.Errorf("tebex secret is required")
	}
	if tebexSecret == "explicit_ignore" {
		// Used in tilt to ignore the secret
		tebexSecret = ""
		params.Log.Info("tebex secret is explicitly ignored")
	}

	s := &server{
		log:         params.Log.With("handler", "payments"),
		tebexSecret: []byte(tebexSecret),
		producer:    params.Producer,
		store:       params.Store,
		authClient:  params.Authz,
		posthog:     params.Posthog,
	}

	params.Lifecycle.Append(fx.Hook{
		OnStart: func(_ context.Context) error {
			ctx, cancel := context.WithCancel(context.Background())
			s.cancelConsumerCtx = cancel
			go s.processStoredEventStream(ctx, params.ReaderFactory.NewReader(payments.TebexMessageTopic), func() {
				// Shutdown used in very bad cases where we have a really major problem
				// (ie kafka totally down will send this service into a restart loop)
				// Not sure this is good behavior it would be better if it just stopped processing kafka messages but idk.
				s.log.Info("shutting down via tebex message failure")
				_ = params.Shutdowner.Shutdown()
			})
			return nil
		},
		OnStop: func(_ context.Context) error {
			s.cancelConsumerCtx()
			return nil
		},
	})

	return s, nil
}

type server struct {
	log         *zap.SugaredLogger
	tebexSecret []byte

	cancelConsumerCtx func()

	producer   wkafka.SyncWriter
	store      *db.Store
	authClient authz.Client
	posthog    posthog.Client
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

	kafkaRecord := kafka.Message{
		Topic: payments.TebexMessageTopic,
		Time:  event.Date,
		Key:   []byte(payments.GetEventTarget(event.Subject)),
		Value: body,
	}
	if err = s.producer.WriteMessages(r.Context(), kafkaRecord); err != nil {
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
