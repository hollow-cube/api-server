package intnl

import (
	"context"
	"errors"
	"fmt"

	"github.com/hollow-cube/hc-services/services/session-service/internal/pkg/model"
	"github.com/hollow-cube/hc-services/services/session-service/internal/pkg/payments"
	"github.com/hollow-cube/hc-services/services/session-service/internal/playerdb"
)

func (s *server) Faucet(ctx context.Context, request FaucetRequestObject) (FaucetResponseObject, error) {
	var currency playerdb.CurrencyType
	if request.Body.Type == nil || *request.Body.Type == "coins" {
		currency = playerdb.Coins // Default value also
	} else if request.Body.Type != nil && *request.Body.Type == "cubits" {
		currency = playerdb.Cubits
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
	newBalance, err := s.store.AddCurrency(ctx, request.Body.PlayerId, currency,
		request.Body.Amount, playerdb.BalanceChangeReasonFaucet, meta)
	if err != nil {
		return nil, fmt.Errorf("failed to add currency: %w", err)
	}

	// Send update to the player to update their stats in-game
	updateMessage := &model.PlayerDataUpdateMessage{
		Action: model.PlayerDataUpdate_Modify,
		Id:     request.Body.PlayerId,
	}
	if currency == playerdb.Coins {
		updateMessage.Coins = &newBalance
	} else {
		updateMessage.Cubits = &newBalance
	}
	if err = s.sendPlayerDataUpdateMessage(ctx, updateMessage); err != nil {
		s.log.Errorw("failed to write player data update", "error", err)
	}

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
	err := playerdb.TxNoReturn(ctx, s.store, func(ctx context.Context, tx *playerdb.Store) error {
		if request.Body.Coins != nil && *request.Body.Coins > 0 {
			newCoins, err := tx.AddCurrency(ctx, request.PlayerId, playerdb.Coins,
				-*request.Body.Coins, playerdb.BalanceChangeReasonBuyCosmetic, meta)
			if err != nil {
				return fmt.Errorf("failed to subtract coins: %w", err)
			}
			update.Coins = &newCoins
		}

		if request.Body.Cubits != nil && *request.Body.Cubits > 0 {
			newCubits, err := tx.AddCurrency(ctx, request.PlayerId, playerdb.Cubits,
				-*request.Body.Cubits, playerdb.BalanceChangeReasonBuyCosmetic, meta)
			if err != nil {
				return fmt.Errorf("failed to subtract cubits: %w", err)
			}
			update.Cubits = &newCubits
		}

		if len(backpackUpdate) > 0 {
			panic("backpack changes not supported anymore :(")
		}

		// Finally, actually add the cosmetic
		err := tx.UnlockCosmetic(ctx, request.PlayerId, request.Body.CosmeticId)
		if err != nil {
			return fmt.Errorf("failed to unlock cosmetic: %w", err)
		}

		return nil
	})
	if errors.Is(err, playerdb.ErrBalanceTooLow) {
		return BuyCosmetic409Response{}, nil
	} else if err != nil {
		return nil, err
	}

	// Send update to the player to update their stats in-game
	if err = s.sendPlayerDataUpdateMessage(ctx, update); err != nil {
		s.log.Errorw("failed to write player data update", "error", err)
	}

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
	err := playerdb.TxNoReturn(ctx, s.store, func(ctx context.Context, tx *playerdb.Store) error {
		if request.Body.Change.Coins != nil && *request.Body.Change.Coins > 0 {
			newCoins, err := tx.AddCurrency(ctx, request.PlayerId, playerdb.Coins, *request.Body.Change.Coins, playerdb.BalanceChangeReasonGiveItemGeneric, request.Body.TxMeta)
			if err != nil {
				return fmt.Errorf("failed to subtract coins: %w", err)
			}
			newState.Coins = &newCoins
		}

		if request.Body.Change.Cubits != nil && *request.Body.Change.Cubits > 0 {
			newCubits, err := tx.AddCurrency(ctx, request.PlayerId, playerdb.Cubits, -*request.Body.Change.Cubits, playerdb.BalanceChangeReasonGiveItemGeneric, request.Body.TxMeta)
			if err != nil {
				return fmt.Errorf("failed to subtract cubits: %w", err)
			}
			newState.Cubits = &newCubits
		}

		if request.Body.Change.Exp != nil && *request.Body.Change.Exp > 0 {
			newExp, err := tx.AddExperience(ctx, request.PlayerId, *request.Body.Change.Exp)
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
	if errors.Is(err, playerdb.ErrBalanceTooLow) {
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
	pd, err := s.store.GetPlayerData(ctx, request.PlayerId)
	if errors.Is(err, playerdb.ErrNoRows) {
		return GetPlayerHypercube404Response{}, nil
	} else if err != nil {
		return nil, fmt.Errorf("failed to get hypercube stats: %w", err)
	}

	return GetPlayerHypercube200JSONResponse{
		Exp:   0,
		Since: pd.HypercubeStart,
		Until: pd.HypercubeEnd,
	}, nil
}

var upgradeMap = map[string]struct{ slots, size int }{
	"map_slot_3": {slots: 1},
	"map_slot_4": {slots: 2},
	"map_slot_5": {slots: 3},
	"map_size_2": {size: 1},
	"map_size_3": {size: 2},
	"map_size_4": {size: 3},
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

	upgrade, ok := upgradeMap[request.Body.UpgradeId]
	if !ok {
		return BuyNamedUpgrade404Response{}, nil
	}

	err := playerdb.TxNoReturn(ctx, s.store, func(ctx context.Context, tx *playerdb.Store) error {

		if request.Body.Cubits != nil && *request.Body.Cubits > 0 {
			newCubits, err := tx.AddCurrency(ctx, request.PlayerId, playerdb.Cubits, -*request.Body.Cubits,
				playerdb.BalanceChangeReasonBuyCosmetic, meta)
			if err != nil {
				return fmt.Errorf("failed to subtract cubits: %w", err)
			}
			update.Cubits = &newCubits
		}

		// will set to max(current, given) so safe to set the 0s
		if err := tx.SetPlayerUnlocks(ctx, request.PlayerId, int16(upgrade.slots), int16(upgrade.size)); err != nil {
			return fmt.Errorf("failed to set player unlocks: %w", err)
		}

		return nil
	})
	if errors.Is(err, playerdb.ErrBalanceTooLow) {
		return BuyNamedUpgrade409Response{}, nil
	} else if err != nil {
		return nil, err
	}

	// Send update to the player to update their stats in-game (though it will be predicted by the response from this function)
	if err = s.sendPlayerDataUpdateMessage(ctx, update); err != nil {
		s.log.Errorw("failed to write player data update", "error", err)
	}

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

	err = s.store.CreatePendingTransaction(ctx, playerId, request.Body.Username, checkoutId)
	if err != nil {
		return nil, fmt.Errorf("failed to create pending transaction: %w", err)
	}

	// Create the basket on tebex async, it will be fetched by the browser using the related public endpoint.
	go payments.CreateBasket(s.tbHeadless, s.store, checkoutId, packageId, request.Body.Username, request.Body.CreatorCode, ips[0])

	return TebexCheckout200JSONResponse{Url: url}, nil
}
