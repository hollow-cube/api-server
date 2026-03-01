package v4Internal

import (
	"context"

	"github.com/hollow-cube/api-server/internal/interaction"
)

type (
	Command             = interaction.Command
	Interaction         = interaction.Interaction
	InteractionResponse = interaction.InteractionResponse
)

// GET /interactions/commands
func (s *Server) GetCommands(ctx context.Context) ([]*Command, error) {
	return s.interactions.Commands, nil
}

// POST /interactions
func (s *Server) ExecuteInteraction(ctx context.Context, body Interaction) (*InteractionResponse, error) {
	return s.interactions.ExecuteInteraction(ctx, body)
}
