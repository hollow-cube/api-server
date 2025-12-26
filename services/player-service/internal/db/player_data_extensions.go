package db

import (
	"context"
	"errors"
	"fmt"
)

func (s *Store) AddExperience(ctx context.Context, iD string, experience int) (int, error) {
	result, err := s.addExperience(ctx, iD, experience)
	if err != nil {
		return 0, err
	}

	go s.metrics.Write(&ExpChanged{
		PlayerId: iD,
		Delta:    experience,
		NewValue: result,
	})

	return result, err
}

var ErrBalanceTooLow = errors.New("balance too low")

func (s *Store) AddCurrency(
	ctx context.Context, playerId string,
	currencyType CurrencyType, amount int,
	reason BalanceChangeReason, meta map[string]interface{},
) (newBalance int, err error) {
	if meta == nil {
		meta = map[string]interface{}{}
	}

	newBalances, err := Tx(ctx, s, func(ctx context.Context, tx *Store) (newBalance []int, err error) {
		err = tx.Unsafe_AppendTxLog(ctx, Unsafe_AppendTxLogParams{
			PlayerID: playerId,
			Reason:   string(reason),
			Currency: currencyType.String(),
			Amount:   amount,
			Meta:     meta,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to insert into tx_log: %w", err)
		}

		if currencyType == Coins {
			return tx.Unsafe_AddCoins(ctx, amount, playerId)
		} else if currencyType == Cubits {
			return tx.Unsafe_AddCubits(ctx, amount, playerId)
		} else {
			panic("invalid currency type " + currencyType.String())
		}
	})
	if err != nil {
		return
	}
	if len(newBalances) < 1 {
		return 0, ErrBalanceTooLow
	}

	newBalance = newBalances[0]
	go func() {
		switch currencyType {
		case Coins:
			s.metrics.Write(&CoinBalanceChanged{
				PlayerId: playerId,
				Delta:    amount,
				NewValue: newBalance,
			})
		case Cubits:
			s.metrics.Write(&CubitBalanceChanged{
				PlayerId: playerId,
				Delta:    amount,
				NewValue: newBalance,
			})
		}
	}()
	return
}
