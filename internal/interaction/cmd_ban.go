package interaction

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/hollow-cube/api-server/internal/pkg/model"
	"github.com/hollow-cube/api-server/internal/playerdb"
	"github.com/hollow-cube/api-server/pkg/hog"
	"github.com/hollow-cube/api-server/pkg/player"
)

func banCommand(ladderAliases map[string]*model.PunishmentLadder) command {
	var ladderChoices []string
	for alias, ladder := range ladderAliases {
		if ladder.Type != model.PunishmentTypeBan {
			continue
		}

		ladderChoices = append(ladderChoices, alias)
	}

	return command{
		Command: Command{
			Name:        "ban",
			Description: "Ban a player from the server",
			Permissions: strconv.FormatUint(player.FlagGenericStaff, 10),

			Arguments: []Argument{
				{Type: ArgumentPlayer, Name: "target"},
				{Type: ArgumentChoice, Name: "ladder", Choices: ladderChoices},
				{Type: ArgumentString, Name: "reason"},
			},
		},

		handler: (*Handler).handleBan,
	}
}

func (h *Handler) handleBan(ctx context.Context, i *Interaction) (*InteractionResponse, error) {
	var target string
	var ladder *model.PunishmentLadder
	reason := "unspecified"

	for _, arg := range i.Command.Arguments {
		switch arg.Name {
		case "target":
			target = arg.Value.(string)
		case "ladder":
			ladder = h.ladderAliases[arg.Value.(string)]
		case "reason":
			reason = arg.Value.(string)
		}
	}
	if target == "" || ladder == nil {
		return nil, fmt.Errorf("invalid arguments") // sanity
	}

	// Check if the player is already punished with this type
	_, err := h.playerStore.GetActivePunishment(ctx, "ban", target)
	if err != nil && !errors.Is(err, playerdb.ErrNoRows) {
		return nil, fmt.Errorf("failed to get active punishment: %w", err)
	} else if err == nil {
		return &InteractionResponse{
			Type: InteractionResponseMessage,
			// TODO: better message using translation key
			StyledText: fmt.Sprintf("<red>Player %s is already banned", target),
		}, nil
	}

	expiresAt, err := h.computeLadderedExpiration(ctx, target, ladder)
	if err != nil {
		return nil, fmt.Errorf("failed to compute laddered expiration: %w", err)
	}

	punishment, err := h.playerStore.CreatePunishment(ctx, playerdb.CreatePunishmentParams{
		PlayerID:   target,
		ExecutorID: i.PlayerID,
		Type:       "ban",
		LadderID:   &ladder.Id,
		Comment:    reason,
		ExpiresAt:  expiresAt,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create punishment: %w", err)
	}

	err = h.jetStream.PublishJSONAsync(ctx, model.PunishmentUpdateMessage{
		Action:     model.PunishmentUpdateAction_Create,
		Punishment: &punishment,
		Message:    model.FormatPunishmentMessage(&punishment),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to publish punishment create message: %w", err)
	}

	hog.Enqueue(hog.Capture{
		Event: "player_banned",
		Properties: hog.NewProperties().
			Set("executor_id", i.PlayerID).
			Set("player_id", target).
			Set("ladder_id", ladder.Id),
	})

	return &InteractionResponse{
		Type: InteractionResponseMessage,
		// TODO: better message using translation key
		StyledText: fmt.Sprintf("<Green>Player %s banned", target),
	}, nil
}

func (h *Handler) computeLadderedExpiration(ctx context.Context, target string, ladder *model.PunishmentLadder) (expiresAt *time.Time, err error) {
	punishments, err := h.playerStore.SearchPunishments(ctx, playerdb.SearchPunishmentsParams{
		Type:     "ban",
		PlayerID: target,
		LadderID: &ladder.Id,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to search punishments: %w", err)
	}

	index := 0
	for _, punishment := range punishments {
		if punishment.CreatedAt.Before(time.Now().Add(-6 * 31 * 24 * time.Hour)) {
			// Ignore punishments older than 6 months
			continue
		}

		index++
	}
	entry := ladder.Entries[min(index, len(ladder.Entries)-1)]
	if entry == nil {
		return nil, fmt.Errorf("ladder entry not found: %s/%d", ladder.Id, index)
	}

	if entry.Duration > 0 {
		expiresAt = new(time.Now().Add(time.Duration(entry.Duration) * time.Second))
	}

	// Otherwise it is permanent so should not have an expiration set
	return
}
