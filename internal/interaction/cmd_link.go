package interaction

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/hollow-cube/api-server/internal/playerdb"
)

var linkCommand = &command{
	Command: Command{
		Name: "link",

		Arguments: []Argument{
			{Type: ArgumentWord, Name: "secret", Optional: true},
		},
	},

	handler: (*Handler).handleLink,
}

func (h *Handler) handleLink(ctx context.Context, i *Interaction) (*Response, error) {
	var secret string
	if len(i.Command.Arguments) > 0 {
		secret = i.Command.Arguments[0].Value.(string)
	}
	if secret == "" {
		return translate("command.link.help"), nil
	}

	pending, err := h.playerStore.GetPendingVerificationBySecret(ctx, "discord", secret)
	if errors.Is(err, playerdb.ErrNoRows) {
		return translate("command.link.invalid_secret"), nil
	} else if err != nil {
		return nil, fmt.Errorf("failed to fetch pending verification: %w", err)
	}
	if pending.Expiration.UnixNano() <= time.Now().UnixNano() {
		err = h.playerStore.DeletePendingVerificationBySecret(ctx, "discord", secret)
		if err != nil {
			h.log.Errorw("Couldn't delete expired verification record: %w", err)
		}

		return translate("command.link.expired_secret"), nil
	}

	// Ensure there is not already an account linked with this discord account
	_, err = h.playerStore.LookupPlayerDataBySocialId(ctx, "discord", pending.UserID)
	if !errors.Is(err, playerdb.ErrNoRows) {
		return translate("command.link.already_linked"), nil
	}

	// Delete the pending verification and add the linked account
	err = playerdb.TxNoReturn(ctx, h.playerStore, func(ctx context.Context, tx *playerdb.Store) (err error) {
		err = tx.DeletePendingVerificationBySecret(ctx, "discord", secret)
		if err != nil {
			return fmt.Errorf("failed to delete pending verification: %w", err)
		}

		err = tx.AddLinkedAccount(ctx, "discord", i.PlayerID, pending.UserID)
		if err != nil {
			return fmt.Errorf("failed to add linked account: %w", err)
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return translate("command.link.success"), nil
}
