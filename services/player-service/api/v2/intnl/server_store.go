package intnl

import (
	"context"
	"errors"
	"fmt"

	"github.com/hollow-cube/hc-services/services/player-service/internal/pkg/authz"
	"github.com/hollow-cube/hc-services/services/player-service/internal/pkg/model"
	"github.com/hollow-cube/hc-services/services/player-service/internal/pkg/payments"
	"github.com/hollow-cube/hc-services/services/player-service/internal/pkg/storage"
)

var errBalanceTooLow = errors.New("balance too low for transaction")

func (s *server) Faucet(ctx context.Context, request FaucetRequestObject) (FaucetResponseObject, error) {
	var currency model.CurrencyType
	if request.Body.Type == nil || *request.Body.Type == "coins" {
		currency = model.Coins // Default value also
	} else if request.Body.Type != nil && *request.Body.Type == "cubits" {
		currency = model.Cubits
	} else if request.Body.Type != nil {
		return Faucet400JSONResponse{BadRequestJSONResponse{
			Message: fmt.Sprintf("invalid currency type: %s", *request.Body.Type),
		}}, nil
	}

	if pExists, err := s.store.PlayerExistsById(ctx, request.Body.PlayerId); err != nil {
		return nil, fmt.Errorf("failed to lookup player: %w", err)
	} else if !pExists {
		return PlayerNotFoundResponse{}, nil
	}

	meta := map[string]interface{}{}
	newBalance, err := s.storageClient.AddCurrency(ctx, request.Body.PlayerId, currency,
		request.Body.Amount, model.BalanceChangeReasonFaucet, meta)
	if err != nil {
		return nil, fmt.Errorf("failed to add currency: %w", err)
	}

	// Send update to the player to update their stats in-game
	updateMessage := &model.PlayerDataUpdateMessage{
		Action: model.PlayerDataUpdate_Modify,
		Id:     request.Body.PlayerId,
	}
	if currency == model.Coins {
		updateMessage.Coins = &newBalance
	} else {
		updateMessage.Cubits = &newBalance
	}
	sendPlayerDataUpdateMessage(s.producer, ctx, updateMessage)

	s.log.Infow("faucet", "player", request.Body.PlayerId, "amount", request.Body.Amount,
		"currency", currency, "new_balance", newBalance)

	return Faucet200Response{}, nil
}

func (s *server) BuyCosmetic(ctx context.Context, request BuyCosmeticRequestObject) (BuyCosmeticResponseObject, error) {
	// Ensure the player exists first because the following requests are not valid otherwise
	if pExists, err := s.store.PlayerExistsById(ctx, request.PlayerId); err != nil {
		return nil, fmt.Errorf("failed to lookup player: %w", err)
	} else if !pExists {
		return PlayerNotFoundResponse{}, nil
	}

	meta := map[string]interface{}{"cosmetic": request.Body.CosmeticId, "raw": request.Body}

	var backpackUpdate model.PlayerBackpack
	if request.Body.Items != nil && len(*request.Body.Items) > 0 {
		backpackUpdate = make(model.PlayerBackpack)
		for itemName, rawCount := range *request.Body.Items {
			item := model.BackpackItem(itemName)
			count, ok := rawCount.(float64)
			if !ok {
				return nil, fmt.Errorf("invalid count for item %s: %v", itemName, rawCount)
			}
			backpackUpdate[item] = int(count)
		}
	}

	update := &model.PlayerDataUpdateMessage{
		Action: model.PlayerDataUpdate_Modify,
		Id:     request.PlayerId,
		// Filled during transaction
	}

	// Do all the updates as a transaction
	err := s.storageClient.RunTransaction(ctx, func(ctx context.Context) error {
		if request.Body.Coins != nil && *request.Body.Coins > 0 {
			newCoins, err := s.storageClient.AddCurrency(ctx, request.PlayerId, model.Coins,
				-*request.Body.Coins, model.BalanceChangeReasonBuyCosmetic, meta)
			if errors.Is(err, storage.ErrBalanceTooLow) {
				return errBalanceTooLow
			} else if err != nil {
				return fmt.Errorf("failed to subtract coins: %w", err)
			}
			update.Coins = &newCoins
		}

		if request.Body.Cubits != nil && *request.Body.Cubits > 0 {
			newCubits, err := s.storageClient.AddCurrency(ctx, request.PlayerId, model.Cubits,
				-*request.Body.Cubits, model.BalanceChangeReasonBuyCosmetic, meta)
			if errors.Is(err, storage.ErrBalanceTooLow) {
				return errBalanceTooLow
			} else if err != nil {
				return fmt.Errorf("failed to subtract cubits: %w", err)
			}
			update.Cubits = &newCubits
		}

		if len(backpackUpdate) > 0 {
			panic("backpack changes not supported anymore :(")
		}

		// Finally, actually add the cosmetic
		err := s.storageClient.UnlockCosmetic(ctx, request.PlayerId, request.Body.CosmeticId)
		if err != nil {
			return fmt.Errorf("failed to unlock cosmetic: %w", err)
		}

		return nil
	})
	if errors.Is(err, errBalanceTooLow) {
		return BuyCosmetic409Response{}, nil
	} else if err != nil {
		return nil, err
	}

	// Send update to the player to update their stats in-game
	sendPlayerDataUpdateMessage(s.producer, ctx, update)

	go s.metrics.Write(&model.CosmeticUnlocked{
		PlayerId: request.PlayerId,
		Cosmetic: request.Body.CosmeticId,
	})
	return BuyCosmetic200Response{}, nil
}

func (s *server) GivePlayerItems(ctx context.Context, request GivePlayerItemsRequestObject) (GivePlayerItemsResponseObject, error) {
	// Ensure the player exists first because the following requests are not valid otherwise
	if pExists, err := s.store.PlayerExistsById(ctx, request.PlayerId); err != nil {
		return nil, fmt.Errorf("failed to lookup player: %w", err)
	} else if !pExists {
		return PlayerNotFoundResponse{}, nil
	}

	var backpackUpdate model.PlayerBackpack
	if request.Body.Change.Backpack != nil && len(*request.Body.Change.Backpack) > 0 {
		backpackUpdate = make(model.PlayerBackpack)
		for itemName, rawCount := range *request.Body.Change.Backpack {
			item := model.BackpackItem(itemName)
			count, ok := rawCount.(float64)
			if !ok {
				return nil, fmt.Errorf("invalid count for item %s: %v", itemName, rawCount)
			}
			backpackUpdate[item] = int(count)
		}
	}

	newState := PlayerInventory{}

	// Do all the updates as a transaction
	err := s.storageClient.RunTransaction(ctx, func(ctx context.Context) error {
		if request.Body.Change.Coins != nil && *request.Body.Change.Coins > 0 {
			newCoins, err := s.storageClient.AddCurrency(ctx, request.PlayerId, model.Coins, *request.Body.Change.Coins, model.BalanceChangeReasonGiveItemGeneric, request.Body.TxMeta)
			if errors.Is(err, storage.ErrBalanceTooLow) {
				return errBalanceTooLow
			} else if err != nil {
				return fmt.Errorf("failed to subtract coins: %w", err)
			}
			newState.Coins = &newCoins
		}

		if request.Body.Change.Cubits != nil && *request.Body.Change.Cubits > 0 {
			newCubits, err := s.storageClient.AddCurrency(ctx, request.PlayerId, model.Cubits, -*request.Body.Change.Cubits, model.BalanceChangeReasonGiveItemGeneric, request.Body.TxMeta)
			if errors.Is(err, storage.ErrBalanceTooLow) {
				return errBalanceTooLow
			} else if err != nil {
				return fmt.Errorf("failed to subtract cubits: %w", err)
			}
			newState.Cubits = &newCubits
		}

		if request.Body.Change.Exp != nil && *request.Body.Change.Exp > 0 {
			newExp, err := s.store.AddExperience(ctx, request.PlayerId, *request.Body.Change.Exp)
			if err != nil {
				return fmt.Errorf("failed to add experience: %w", err)
			}
			newState.Exp = &newExp
		}

		if len(backpackUpdate) > 0 {
			panic("backpack changes not supported anymore :(")
		}

		return nil
	})
	if errors.Is(err, errBalanceTooLow) {
		return GivePlayerItems404Response{}, nil
	} else if err != nil {
		return nil, err
	}

	return GivePlayerItems200JSONResponse{
		Diff:     request.Body.Change,
		NewState: newState,
	}, nil
}

func (s *server) GetPlayerHypercube(ctx context.Context, request GetPlayerHypercubeRequestObject) (GetPlayerHypercubeResponseObject, error) {
	startTime, term, err := s.authzClient.GetHypercubeStats(ctx, request.PlayerId, authz.NoKey)
	if errors.Is(err, authz.ErrNotFound) {
		return GetPlayerHypercube404Response{}, nil
	} else if err != nil {
		return nil, fmt.Errorf("failed to get hypercube stats: %w", err)
	}

	until := startTime.Add(term)
	return GetPlayerHypercube200JSONResponse{
		Exp:   0,
		Since: &startTime,
		Until: &until,
	}, nil
}

func (s *server) BuyNamedUpgrade(ctx context.Context, request BuyNamedUpgradeRequestObject) (BuyNamedUpgradeResponseObject, error) {
	// Ensure the player exists first because the following requests are not valid otherwise
	if pExists, err := s.store.PlayerExistsById(ctx, request.PlayerId); err != nil {
		return nil, fmt.Errorf("failed to lookup player: %w", err)
	} else if !pExists {
		return PlayerNotFoundResponse{}, nil
	}

	meta := map[string]interface{}{"upgrade": request.Body.UpgradeId, "raw": request.Body}

	update := &model.PlayerDataUpdateMessage{
		Action: model.PlayerDataUpdate_Modify,
		Id:     request.PlayerId,
		// Filled during transaction
	}

	// We need to update SpiceDB, so we apply the change as a 2-phase commit

	if err := s.authzClient.UnlockUpgrade(ctx, request.PlayerId, request.Body.UpgradeId, authz.NoKey); err != nil { // 2pc: Update SpiceDB
		return nil, fmt.Errorf("failed to unlock upgrade: %w", err)
	}
	err := s.storageClient.RunTransaction(ctx, func(ctx context.Context) error { // 2pc: Begin transaction

		if request.Body.Cubits != nil && *request.Body.Cubits > 0 {
			newCubits, err := s.storageClient.AddCurrency(ctx, request.PlayerId, model.Cubits, -*request.Body.Cubits,
				model.BalanceChangeReasonBuyCosmetic, meta)
			if errors.Is(err, storage.ErrBalanceTooLow) {
				return errBalanceTooLow
			} else if err != nil {
				return fmt.Errorf("failed to subtract cubits: %w", err)
			}
			update.Cubits = &newCubits
		}

		return nil
	}) // 2pc: Commit transaction
	if errors.Is(err, errBalanceTooLow) {
		return BuyNamedUpgrade409Response{}, nil
	} else if err != nil {
		if errRevert := s.authzClient.RemoveUpgrade(ctx, request.PlayerId, request.Body.UpgradeId, authz.NoKey); errRevert != nil { // 2pc: Revert SpiceDB
			s.log.Errorw("Failed to revert upgrade unlock", "player", request.PlayerId,
				"upgrade", request.Body.UpgradeId, "err", errRevert)
		}

		return nil, err
	}

	sendPlayerDataUpdateMessage(s.producer, ctx, update) // Send update to the player to update their stats in-game (though it will be predicted by the response from this function)

	return BuyNamedUpgrade200Response{}, nil
}

func (s *server) TebexCheckout(ctx context.Context, request TebexCheckoutRequestObject) (TebexCheckoutResponseObject, error) {
	packageId, ok := payments.PackageNameMap[request.Body.Package]
	if !ok {
		return nil, fmt.Errorf("invalid package: %s", request.Body.Package)
	}

	playerId, err := s.store.LookupPlayerByUsername(ctx, request.Body.Username)
	if err != nil {
		return nil, fmt.Errorf("failed to lookup player: %w", err)
	}
	ips, err := s.store.GetPlayerIPHistory(ctx, playerId)
	if err != nil {
		return nil, fmt.Errorf("failed to get player ips: %w", err)
	} else if len(ips) == 0 {
		// We can't proceed properly without an IP, but we can just redirect to the store anyway.
		return TebexCheckout200JSONResponse{Url: "https://hollowcube.net/store"}, nil
	}

	checkoutId := genVerifySecret()
	url := fmt.Sprintf("https://hollowcube.net/store?c=%s", checkoutId)

	err = s.storageClient.CreatePendingTransaction(ctx, checkoutId, playerId, request.Body.Username)
	if err != nil {
		return nil, fmt.Errorf("failed to create pending transaction: %w", err)
	}

	// Create the basket on tebex async, it will be fetched by the browser using the related public endpoint.
	go payments.CreateBasket(s.tbHeadless, s.storageClient, checkoutId, packageId, request.Body.Username, request.Body.CreatorCode, ips[0])

	return TebexCheckout200JSONResponse{Url: url}, nil
}
