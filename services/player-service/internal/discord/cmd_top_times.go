package discord

import (
	"context"
	"fmt"

	"github.com/bwmarrin/discordgo"
)

// TODO: enable after map/player merged so this can be returned in a sane way.
var topTimesCommand = Command{
	ApplicationCommand: discordgo.ApplicationCommand{
		Name:        "toptimes",
		Description: "View the top leaderboard times of a map",
		Contexts: &[]discordgo.InteractionContextType{
			discordgo.InteractionContextGuild,
			discordgo.InteractionContextBotDM,
			discordgo.InteractionContextPrivateChannel,
		},

		Options: []*discordgo.ApplicationCommandOption{
			{
				Name:         "map",
				Description:  "The map to search times for",
				Type:         discordgo.ApplicationCommandOptionString,
				Required:     true,
				Autocomplete: true,
			},
		},
	},

	deferred: false,
	handler:  (*Handler).handleTopTimes,
}

func (h *Handler) handleTopTimes(ctx context.Context, i *discordgo.Interaction) (*discordgo.InteractionResponse, error) {
	//userId, _ := getUserInfo(i)

	return &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Flags:   discordgo.MessageFlagsEphemeral,
			Content: fmt.Sprintf("TODO"),
		},
	}, nil
}
