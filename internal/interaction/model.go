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

type Type string

const (
	TypeCommand Type = "command"
)

type Interaction struct {
	ID   string `json:"id"` // Identifier of target, eg command name for command
	Type Type   `json:"type"`

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

type ResponseType string

const (
	ResponseMessage ResponseType = "message"
)

type Response struct {
	Type ResponseType `json:"type"`

	StyledText string `json:"styledText,omitempty"` // Minimessage string

	// Not totally sure on the format here, will need some work...
	TranslationKey  string   `json:"translationKey,omitempty"`
	TranslationArgs []string `json:"translationArgs,omitempty"`
}

func translate(key string, args ...string) *Response {
	return &Response{
		Type:            ResponseMessage,
		TranslationKey:  key,
		TranslationArgs: args,
	}
}
