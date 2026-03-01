package interaction

type Command struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Permissions string `json:"permissions"`

	Arguments []Argument `json:"arguments"`
}

type ArgumentType string

const (
	ArgumentWord   ArgumentType = "word"
	ArgumentString ArgumentType = "string"
	ArgumentChoice ArgumentType = "choice"
	ArgumentPlayer ArgumentType = "player"
)

type Argument struct {
	Type     ArgumentType `json:"type"`
	Name     string       `json:"name"`
	Optional bool         `json:"optional,omitempty"`

	Choices []string `json:"choices,omitempty"`
}

type InteractionType string

const (
	InteractionCommand InteractionType = "command"
)

type Interaction struct {
	ID   string          `json:"id"` // Identifier of target, eg command name for command
	Type InteractionType `json:"type"`

	PlayerID string `json:"playerId"` // Sender

	Command *CommandInteractionData `json:"command,omitempty"` // Only present for Type = InteractionCommand
}

type CommandInteractionData struct {
	Arguments []CommandInteractionArgument `json:"arguments"`
}

type CommandInteractionArgument struct {
	Name  string       `json:"name"`
	Type  ArgumentType `json:"type"`
	Value any          `json:"value"` // Depends on Type
}

type InteractionResponseType string

const (
	InteractionResponseMessage InteractionResponseType = "message"
)

type InteractionResponse struct {
	Type InteractionResponseType `json:"type"`

	StyledText string `json:"styledText,omitempty"` // Minimessage string
}
