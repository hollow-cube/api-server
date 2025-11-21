package storage

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/hollow-cube/hc-services/services/player-service/internal/pkg/model"
	"github.com/jackc/pgx/v5"
)

func (c *PostgresClient) UpdateBackpack(ctx context.Context, playerId string, relativeBackpack model.PlayerBackpack) (model.PlayerBackpack, error) {
	if len(relativeBackpack) == 0 {
		return make(model.PlayerBackpack), nil
	}

	var fieldList strings.Builder
	var variableList strings.Builder
	var updateList strings.Builder
	var whereList strings.Builder
	quantities := make([]*int, len(relativeBackpack))
	scanEntries := make([]any, len(relativeBackpack))
	var i = 0
	var sign = 0
	for item, relativeUpdate := range relativeBackpack {
		if relativeUpdate == 0 {
			continue
		}
		if sign == 0 {
			sign = relativeUpdate
		}
		if sign > 0 && relativeUpdate < 0 {
			return nil, errors.New("cannot mix positive and negative values")
		}
		if i > 0 {
			fieldList.WriteString(",")
			variableList.WriteString(",")
			updateList.WriteString(",")
			whereList.WriteString(" and ")
		}

		fieldList.WriteString(string(item))
		variableList.WriteString(strconv.Itoa(relativeUpdate))
		if sign > 0 {
			updateList.WriteString(fmt.Sprintf("%s = least(player_backpack.%s + %d, %d)", item, item, relativeUpdate, item.StackSize()))
		} else {
			updateList.WriteString(fmt.Sprintf("%s = least(%s + %d, %d)", item, item, relativeUpdate, item.StackSize()))
			whereList.WriteString(fmt.Sprintf("player_backpack.%s + %d >= 0", item, relativeUpdate))
		}

		var quantity int
		quantities[i] = &quantity
		scanEntries[i] = &quantity
		i++
	}

	var query string
	if sign > 0 {
		query = fmt.Sprintf("insert into public.player_backpack (player_id,%s) values ($1, %s) on conflict(player_id) do update set %s returning %s;",
			fieldList.String(), variableList.String(), updateList.String(), fieldList.String())
	} else {
		query = fmt.Sprintf("update public.player_backpack set %s where player_id = $1 and %s returning %s;",
			updateList.String(), whereList.String(), fieldList.String())
	}
	err := c.safeQueryRow(ctx, query, playerId).Scan(scanEntries...)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrBalanceTooLow
	} else if err != nil {
		return nil, err
	}

	result := make(model.PlayerBackpack, len(model.BackpackItems))
	i = 0
	for item, relativeUpdate := range relativeBackpack {
		if relativeUpdate == 0 {
			continue
		}
		result[item] = *quantities[i]
		i++

		go c.metrics.Write(&model.BackpackEntryChanged{
			PlayerId: playerId,
			Item:     string(item),
			Delta:    relativeUpdate,
			NewValue: result[item],
		})
	}
	return result, nil

}
