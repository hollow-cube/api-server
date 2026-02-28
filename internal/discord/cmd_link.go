package discord

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/hollow-cube/api-server/internal/pkg/model"
	"github.com/hollow-cube/api-server/internal/pkg/util"
	"github.com/hollow-cube/api-server/internal/playerdb"
)

var linkCommand = Command{
	ApplicationCommand: discordgo.ApplicationCommand{
		Name:        "link",
		Description: "Link your Minecraft and Discord accounts",
		Contexts: &[]discordgo.InteractionContextType{
			discordgo.InteractionContextGuild,
			discordgo.InteractionContextBotDM,
			discordgo.InteractionContextPrivateChannel,
		},
	},

	deferred: false,
	handler:  (*Handler).handleLink,
}

func (h *Handler) handleLink(ctx context.Context, i *discordgo.Interaction) (*discordgo.InteractionResponse, error) {
	userId, _ := getUserInfo(i)

	_, err := h.store.LookupPlayerDataBySocialId(ctx, "discord", userId)
	if !errors.Is(err, playerdb.ErrNoRows) {
		return &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Flags:   discordgo.MessageFlagsEphemeral,
				Content: ":bangbang: Your Minecraft account is already linked!",
			},
		}, nil
	}

	const verificationExpirationTimer = 5 * time.Minute

	var secret = util.NewVerifySecret()
	err = h.store.UpsertPendingVerification(ctx, playerdb.UpsertPendingVerificationParams{
		Type:       string(model.VerificationTypeDiscord),
		UserID:     userId,
		UserSecret: secret,
		Expiration: time.Now().Add(verificationExpirationTimer),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create verification record: %w", err)
	}

	return &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Flags:   discordgo.MessageFlagsEphemeral,
			Content: fmt.Sprintf("1. Join **play.hollowcube.net**\n2. Run `/link %s`", secret),
		},
	}, nil
}
