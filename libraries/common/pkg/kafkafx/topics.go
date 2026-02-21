package kafkafx

const (
	//
	// Owned by the Java server
	//

	TopicChatInput = "chat"

	//
	// Owned by the Session service
	//

	TopicSessionUpdates = "session-updates"

	TopicChatOutput        = "chat-messages"
	TopicChatAnnouncements = "chat_announcements"

	TopicMapJoin = "map-join"

	TopicInvites            = "invites"
	TopicInviteAcceptReject = "invite-accept-reject"

	//
	// Owned by the Map service
	//

	TopicMapUpdate = "map_mgmt"

	//
	// Owned by the Player service
	//

	TopicPlayerDataUpdate = "player_data_updates"

	TopicNotificationUpdate = "notification_update"

	TopicPunishmentUpdate = "punishments"

	TopicTebexMessages    = "tebex_messages"
	TopicTebexDlqMessages = "tebex_messages_dlq"
)
