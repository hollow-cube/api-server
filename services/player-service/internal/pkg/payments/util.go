package payments

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/hollow-cube/hc-services/services/player-service/internal/pkg/storage"
	"github.com/hollow-cube/tebex-go"
	"go.uber.org/zap"
)

// GetEventTarget returns the target id of a tebex event
func GetEventTarget(event interface{}) string {
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

func CreateBasket(tbHeadless *tebex.HeadlessClient, storageClient storage.Client, checkoutId string, packageId int, username, creatorCode, ip string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Create the basket
	basket, err := tbHeadless.CreateBasket(ctx, tbWebstoreId, tebex.HeadlessCreateBasketRequest{
		Username:  username,
		IPAddress: ip,
	})
	if err != nil {
		recordCheckoutState(storageClient, checkoutId, "", err)
		return
	}

	// Add a package to the basket
	basket, err = tbHeadless.BasketAddPackage(ctx, basket.Ident, tebex.HeadlessBasketAddPackageRequest{
		PackageId: packageId,
		Quantity:  1,
	})
	if err != nil {
		recordCheckoutState(storageClient, checkoutId, "", fmt.Errorf("failed to add package to basket: %w", err))
		return
	}

	// Add the creator code if present
	if creatorCode != "" {
		err = tbHeadless.BasketApplyCreatorCode(ctx, tbWebstoreId, basket.Ident, creatorCode)
		if errors.Is(err, tebex.ErrHeadlessCreatorCodeNotFound) {
			zap.S().Infow("creator code not found", "code", creatorCode)
		} else if err != nil {
			recordCheckoutState(storageClient, checkoutId, "", fmt.Errorf("failed to add creator code to basket: %w", err))
		}
	}

	// Success :)
	recordCheckoutState(storageClient, checkoutId, basket.Ident, nil)
	zap.S().Infow("created basket", "checkout_id", checkoutId, "basket_id", basket.Ident)
}

func recordCheckoutState(storageClient storage.Client, checkoutId, basketId string, err error) {
	if err != nil {
		zap.S().Errorw("failed to create basket", "checkout_id", checkoutId, "err", err)
		if err = storageClient.ResolvePendingTransaction(context.Background(), checkoutId, ""); err != nil {
			zap.S().Errorw("failed to resolve pending transaction", "checkout_id", checkoutId, "err", err)
		}
	} else {
		if err = storageClient.ResolvePendingTransaction(context.Background(), checkoutId, basketId); err != nil {
			zap.S().Errorw("failed to resolve pending transaction", "checkout_id", checkoutId, "err", err)
		}
	}
}
