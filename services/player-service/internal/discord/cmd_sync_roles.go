package discord

import (
	"context"
	"errors"
	"fmt"

	"github.com/bwmarrin/discordgo"
	"github.com/hollow-cube/hc-services/services/player-service/internal/db"
	"github.com/hollow-cube/hc-services/services/player-service/pkg/player"
)

var syncRolesCommand = Command{
	ApplicationCommand: discordgo.ApplicationCommand{
		Name:                     "Sync Roles",
		Type:                     discordgo.UserApplicationCommand,
		Contexts:                 &[]discordgo.InteractionContextType{discordgo.InteractionContextGuild},
		DefaultMemberPermissions: new(int64(discordgo.PermissionAdministrator)),
	},

	deferred: false,
	handler:  (*Handler).handleSyncRoles,
}

func (h *Handler) handleSyncRoles(ctx context.Context, i *discordgo.Interaction) (*discordgo.InteractionResponse, error) {
	targetId := i.ApplicationCommandData().TargetID

	pd, err := h.store.LookupPlayerDataBySocialId(ctx, "discord", targetId)
	if errors.Is(err, db.ErrNoRows) {
		return &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Flags:   discordgo.MessageFlagsEphemeral,
				Content: fmt.Sprintf(":bangbang: <@%s> has not linked their discord account!", targetId),
			},
		}, nil
	}

	role := pd.EffectiveRole()
	switch role {
	case player.Dev1Role, player.Dev2Role, player.Dev3Role:
	case player.CT1Role, player.CT2Role, player.CT3Role:
	case player.Mod1Role, player.Mod2Role, player.Mod3Role:
	case player.MediaRole:
	case player.HypercubeRole:
	default:
		return &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Flags:   discordgo.MessageFlagsEphemeral,
				Content: fmt.Sprintf(":bangbang: Don't know how to sync `%s`", role),
			},
		}, nil
	}

	return &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Flags:   discordgo.MessageFlagsEphemeral,
			Content: fmt.Sprintf(":white_check_mark: Synced <@%s> (aka %s)  →  `%s`.\n-# not implemented", targetId, pd.Username, role),
		},
	}, nil
}
