package interaction

import (
	"context"
	"fmt"

	"github.com/hollow-cube/api-server/api/auth"
	"github.com/hollow-cube/api-server/internal/playerdb"
)

var apiCommand = &command{
	Command: Command{
		Name: "api",
	},

	handler: (*Handler).handleApi,
}

func (h *Handler) handleApi(ctx context.Context, i *Interaction) (*Response, error) {
	return playerdb.Tx(ctx, h.playerStore, func(ctx context.Context, tx *playerdb.Store) (*Response, error) {
		_, err := tx.GetPlayerData(ctx, i.PlayerID)
		if err != nil {
			return nil, fmt.Errorf("failed to get player data: %w", err)
		}

		err = tx.DeleteAllApiKeys(ctx, i.PlayerID)
		if err != nil {
			return nil, fmt.Errorf("failed to delete existing api keys: %w", err)
		}

		key, hash, err := auth.GenerateAPIKey()
		if err != nil {
			return nil, fmt.Errorf("failed to generate api key: %w", err)
		}

		err = tx.InsertApiKey(ctx, hash, i.PlayerID)
		if err != nil {
			return nil, fmt.Errorf("failed to insert api key: %w", err)
		}

		hiddenKey := key[:8] + "..." + key[len(key)-3:]
		return &Response{
			Type:       ResponseMessage,
			StyledText: fmt.Sprintf("Your API key: <click:copy_to_clipboard:'%s'><hover:show_text:'Click to copy'>%s</hover></click>\n<i>All previous keys have been invalidated</i>", key, hiddenKey),
		}, nil
	})
}
