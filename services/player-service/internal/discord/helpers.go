package discord

import "github.com/bwmarrin/discordgo"

func getUserInfo(i *discordgo.Interaction) (userId, username string) {
	if i.User != nil {
		userId = i.User.ID
		username = i.User.Username
	} else {
		userId = i.Member.User.ID
		username = i.Member.User.Username
	}
	return
}
