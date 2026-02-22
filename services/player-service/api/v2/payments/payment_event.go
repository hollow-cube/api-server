package payments

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/hollow-cube/hc-services/services/player-service/internal/db"
	"github.com/hollow-cube/hc-services/services/player-service/internal/pkg/model"
	"github.com/hollow-cube/hc-services/services/player-service/internal/pkg/payments"
	"github.com/hollow-cube/tebex-go"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/posthog/posthog-go"
)

func (s *server) processStoredEventStream(ctx context.Context, msg jetstream.Msg) error {
	s.log.Infow("read tebex message", "subject", msg.Subject())
	event, err := tebex.ParseEvent(msg.Data())
	if err != nil { // This is really a sanity check because we parsed the message before writing it to nats
		return fmt.Errorf("failed to parse tebex event: %w", err)
	}

	switch sub := event.Subject.(type) {
	case *tebex.PaymentCompletedEvent:
		err = s.handlePaymentCompletedEvent(ctx, event, sub)
	case *tebex.PaymentDeclinedEvent:
		err = logTebexEvent(ctx, s.store, event)
	case *tebex.PaymentRefundedEvent:
		err = s.handlePaymentRefundedEvent(ctx, event, sub)
	case *tebex.PaymentDisputeOpenedEvent:
		err = s.handlePaymentDisputeOpenedEvent(ctx, event, sub)
	case *tebex.PaymentDisputeWonEvent:
		err = s.handlePaymentDisputeWonEvent(ctx, event, sub)
	case *tebex.PaymentDisputeLostEvent:
		err = s.handlePaymentDisputeLostEvent(ctx, event, sub)
	case *tebex.PaymentDisputeClosedEvent:
		err = logTebexEvent(ctx, s.store, event)
	case *tebex.RecurringPaymentStartedEvent:
		err = logTebexEvent(ctx, s.store, event)
	case *tebex.RecurringPaymentRenewedEvent:
		err = logTebexEvent(ctx, s.store, event)
	case *tebex.RecurringPaymentEndedEvent:
		err = logTebexEvent(ctx, s.store, event)
	case *tebex.RecurringPaymentStatusChangedEvent:
		err = logTebexEvent(ctx, s.store, event)
	}

	// If we have an error processing the message for whatever reason, we should put it in the DLQ.
	// todo need to figure out what to do with these, but don't want to completely lose them for now
	if err != nil {
		s.log.Errorw("failed to process tebex event", "error", err)

		// If last attempt, send to DLQ to preserve
		metadata, _ := msg.Metadata()
		if metadata.NumDelivered >= uint64(tebexMaxDeliver) {
			if err = s.jetStream.PublishAsync(ctx, msg.Subject(), msg.Data()); err != nil {
				s.log.Errorw("failed to publish tebex event to dlq", "error", err)
			}
		}
	}

	s.log.Infow("finished handling tebex message...")
	if err = msg.Ack(); err != nil {
		return fmt.Errorf("failed to ack tebex message: %w", err)
	}
	return nil
}

func (s *server) handlePaymentCompletedEvent(ctx context.Context, raw *tebex.Event, event *tebex.PaymentCompletedEvent) error {

	changes, err := s.computeChangeList(ctx, event.Products)
	if err != nil {
		return err
	}

	newBalances, err := s.applyChangeList(ctx, raw, event.TransactionId, changes)
	if err != nil {
		return err
	}

	if err = s.writePurchaseUpdates(ctx, changes, newBalances); err != nil {
		// Log but dont fail over this.
		s.log.Errorw("failed to write purchase updates", "error", err)
	}

	product := event.Products[0]
	props := posthog.NewProperties().
		Set("$ip", event.Customer.Ip).
		Set("amount", int64(event.Price.Amount*100)).
		Set("currency", event.Price.Currency).
		Set("product", product.Name)
	if event.RecurringPaymentReference != nil {
		props.Set("subscription", *event.RecurringPaymentReference)
	}
	eventName := "Payment Completed"
	if product.Id == 6282911 {
		// Kinda dumb, but posthog doesnt allow you to read subscription duration
		// from a property so we need to create a second event to track 1y subscriptions
		eventName += " (Hypercube 1y)"
	}
	_ = s.posthog.Enqueue(posthog.Capture{
		DistinctId: changes[0].Target,
		Event:      eventName,
		Timestamp:  event.CreatedAt,
		Properties: props,
	})

	s.log.Infow("tebex payment completed event processed", "txid", event.TransactionId, "new_balances", newBalances)

	return nil
}

func (s *server) writePurchaseUpdates(ctx context.Context, changes []*model.TebexChange, newBalances map[string]int) error {
	cubitUpdates := make(map[string]int)
	hypercubeUpdates := make(map[string]int)
	for _, change := range changes {
		if change.Type == model.TebexChangeCubits {
			cubitUpdates[change.Target] = hypercubeUpdates[change.Target] + change.Amount
		} else if change.Type == model.TebexChangeHypercube {
			hypercubeUpdates[change.Target] = hypercubeUpdates[change.Target] + change.Amount
		}
	}

	var err error

	// Send out cubit changes
	for playerId, newBalance := range newBalances {
		update := model.PlayerDataUpdateMessage{
			Action: model.PlayerDataUpdate_Modify,
			Id:     playerId,
			Cubits: &newBalance,
			Reason: &model.UpdateReason{
				Type:     model.UpdateReason_Cubits,
				Quantity: cubitUpdates[playerId],
			},
		}
		if err = s.jetStream.PublishJSONAsync(ctx, &update); err != nil {
			return fmt.Errorf("failed to publish player data update message: %w", err)
		}
	}

	// Send out hypercube changes
	for player, hypercubeAdd := range hypercubeUpdates {
		update := model.PlayerDataUpdateMessage{
			Action: model.PlayerDataUpdate_Modify,
			Id:     player,
			Reason: &model.UpdateReason{
				Type:     model.UpdateReason_Hypercube,
				Quantity: hypercubeAdd,
			},
		}
		if err = s.jetStream.PublishJSONAsync(ctx, &update); err != nil {
			return fmt.Errorf("failed to publish player data update message: %w", err)
		}
	}

	return nil
}

func (s *server) handlePaymentRefundedEvent(ctx context.Context, raw *tebex.Event, event *tebex.PaymentRefundedEvent) error {
	s.log.Warnw("ignoring refund event for now")
	return nil
}

// computeChangeList collects a collapsed view of each transaction as well as our internal player ID for each individual purchase.
// Note that we don't necessarily allow gifting on the store, so in theory this will always be a single player,
// but since the API is kinda public, I don't want to risk it.
func (s *server) computeChangeList(ctx context.Context, products []*tebex.ProductPurchase) (changes []*model.TebexChange, err error) {
	playersById := map[string]string{}
	changes = make([]*model.TebexChange, len(products))
	for i, purchase := range products {
		change := &model.TebexChange{}
		changes[i] = change

		// Fetch the player ID
		if id, ok := playersById[purchase.Username.Id]; ok {
			change.Target = id
		} else {
			change.Target, err = s.findPlayer(ctx, purchase.Username.Username)
			if err != nil {
				return nil, fmt.Errorf("failed to find player by username: %w", err)
			}
			playersById[purchase.Username.Id] = change.Target
		}

		// Compute the change
		if hypercubeDuration, ok := payments.HypercubePackages[purchase.Id]; ok {
			change.Type = model.TebexChangeHypercube
			change.Amount = int(hypercubeDuration.Duration.Minutes()) * purchase.Quantity
		} else if cubits, ok := payments.CubitsPackages[purchase.Id]; ok {
			change.Type = model.TebexChangeCubits
			change.Amount = cubits.Amount * purchase.Quantity
		} else {
			return nil, fmt.Errorf("unknown package ID: %d", purchase.Id)
		}
	}

	return
}

func (s *server) applyChangeList(ctx context.Context, rawEvent *tebex.Event, txId string, changes []*model.TebexChange) (map[string]int, error) {
	return db.Tx(ctx, s.store, func(ctx context.Context, tx *db.Store) (newBalances map[string]int, err error) { // 2pc: Begin transaction
		changesJson, err := json.Marshal(changes)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal change list: %w", err)
		}
		createdStates, err := tx.CreateTebexState(ctx, txId, changesJson)
		if err != nil {
			return nil, fmt.Errorf("failed to create tebex state: %w", err)
		} else if createdStates == 0 {
			// This is fine, we already processed this tx.
			return nil, nil
		}

		if err = logTebexEvent(ctx, tx, rawEvent); err != nil {
			return nil, fmt.Errorf("failed to log tebex event: %w", err)
		}

		meta := map[string]interface{}{"tx": txId}
		if newBalances, err = applyCubitsChanges(ctx, tx, changes, meta); err != nil {
			return nil, fmt.Errorf("failed to apply cubits changes: %w", err)
		}

		if err = s.applyHypercubeChanges(ctx, changes); err != nil {
			return nil, fmt.Errorf("failed to apply hypercube changes (pre apply): %w", err)
		}

		return newBalances, nil
	})
}

func applyCubitsChanges(ctx context.Context, store *db.Store, changes []*model.TebexChange, txMeta map[string]interface{}) (newBalances map[string]int, err error) {
	newBalances = map[string]int{}
	for _, change := range changes {
		if change.Type != model.TebexChangeCubits {
			continue
		}

		newBalances[change.Target], err = store.AddCurrency(
			ctx, change.Target, db.Cubits, change.Amount,
			db.BalanceChangeReasonTebexOneoff, txMeta,
		)
		if err != nil {
			return
		}
	}
	return
}

func (s *server) applyHypercubeChanges(ctx context.Context, changes []*model.TebexChange) error {
	for _, change := range changes {
		if change.Type != model.TebexChangeHypercube {
			continue
		}

		err := s.store.AppendHypercube(ctx, change.Target, pgtype.Interval{
			Valid:        true,
			Microseconds: (time.Duration(change.Amount) * time.Minute).Microseconds(),
		})
		if err != nil {
			return err
		}
	}

	return nil
}

func (s *server) handlePaymentDisputeOpenedEvent(ctx context.Context, raw *tebex.Event, event *tebex.PaymentDisputeOpenedEvent) error {
	if err := logTebexEvent(ctx, s.store, raw); err != nil {
		return fmt.Errorf("failed to log tebex event: %w", err)
	}

	s.log.Infow("tebex payment dispute opened event processed", "txid", event.TransactionId)
	return nil
	// If a webhook is not provided, do nothing
	//if s.disputeDiscordWebhook == "" {
	//	s.log.Infow("tebex payment dispute opened event processed", "txid", event.TransactionId)
	//	return nil
	//}

	//return discord.SendWebhookEmbed(ctx, h.disputeDiscordWebhook, &discord.Embed{
	//	Title:     "Payment Dispute Opened >:(",
	//	Thumbnail: discord.EmbedThumbnail{Url: fmt.Sprintf("https://crafatar.com/avatars/%s", event.Customer.Username.Id)},
	//	Color:     0xff0000,
	//	Footer:    discord.EmbedFooter{},
	//	Fields: []discord.EmbedField{
	//		{Name: "Transaction ID", Value: event.TransactionId, Inline: true},
	//		{Name: "Player", Value: event.Customer.Username.Username, Inline: true},
	//		{Name: "Price", Value: fmt.Sprintf("%.2f %s (%s)", event.Price.Amount, event.Price.Currency, event.PaymentMethod.Name), Inline: true},
	//	},
	//})
}

func (s *server) handlePaymentDisputeWonEvent(ctx context.Context, raw *tebex.Event, event *tebex.PaymentDisputeWonEvent) error {
	if err := logTebexEvent(ctx, s.store, raw); err != nil {
		return fmt.Errorf("failed to log tebex event: %w", err)
	}

	//todo do something??
	s.log.Infow("tebex payment dispute won event processed", "txid", event.TransactionId)
	return nil
}

func (s *server) handlePaymentDisputeLostEvent(ctx context.Context, raw *tebex.Event, event *tebex.PaymentDisputeLostEvent) error {
	if err := logTebexEvent(ctx, s.store, raw); err != nil {
		return fmt.Errorf("failed to log tebex event: %w", err)
	}

	//todo ban the player
	s.log.Infow("tebex payment dispute lost event processed", "txid", event.TransactionId)
	return nil
}

// findPlayer will lookup a player by their username.
func (s *server) findPlayer(ctx context.Context, username string) (string, error) {
	return s.store.LookupPlayerByUsername(ctx, username)
}

func extractTargetFromEvent(event interface{}) string {
	switch event := event.(type) {
	case *tebex.PaymentCompletedEvent:
		return event.Customer.Username.Id
	case *tebex.PaymentDeclinedEvent:
		return event.Customer.Username.Id
	case *tebex.PaymentRefundedEvent:
		return event.Customer.Username.Id
	case *tebex.PaymentDisputeOpenedEvent:
		return event.Customer.Username.Id
	case *tebex.PaymentDisputeWonEvent:
		return event.Customer.Username.Id
	case *tebex.PaymentDisputeLostEvent:
		return event.Customer.Username.Id
	case *tebex.PaymentDisputeClosedEvent:
		return event.Customer.Username.Id
	case *tebex.RecurringPaymentStartedEvent:
		return event.InitialPayment.Customer.Username.Id
	case *tebex.RecurringPaymentRenewedEvent:
		return event.InitialPayment.Customer.Username.Id
	case *tebex.RecurringPaymentEndedEvent:
		return event.InitialPayment.Customer.Username.Id
	case *tebex.RecurringPaymentStatusChangedEvent:
		return event.InitialPayment.Customer.Username.Id
	default:
		return "unknown"
	}
}

func (s *server) sendPlayerDataUpdateMessage(ctx context.Context, msg *model.PlayerDataUpdateMessage) error {

	return nil
}

func logTebexEvent(ctx context.Context, store *db.Store, event *tebex.Event) error {
	rawSubject, err := json.Marshal(event.Subject)
	if err != nil {
		return fmt.Errorf("failed to marshal tebex event subject: %w", err)
	}

	if err = store.LogTebexEvent(ctx, event.Id, event.Date, rawSubject); err != nil {
		return fmt.Errorf("failed to log tebex event: %w", err)
	}

	return nil
}
