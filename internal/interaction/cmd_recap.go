package interaction

import (
	"context"
	"errors"
	"fmt"

	"github.com/hollow-cube/api-server/internal/playerdb"
)

var recapCommand = &command{
	Command: Command{
		Name: "recap",
	},

	handler: (*Handler).handleRecap,
}

func (h *Handler) handleRecap(ctx context.Context, i *Interaction) (*Response, error) {
	recap, err := h.playerStore.GetRecapByPlayerId(ctx, i.PlayerID, 2025)
	if errors.Is(err, playerdb.ErrNoRows) {
		return translate("commands.recap.unavailable"), nil
	} else if err != nil {
		return nil, fmt.Errorf("failed to get player recap: %w", err)
	}

	return translate("commands.recap.generated", recap.ID), nil
}
