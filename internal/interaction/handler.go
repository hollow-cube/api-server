package interaction

import (
	"context"

	"github.com/hollow-cube/api-server/config"
	"github.com/hollow-cube/api-server/internal/pkg/model"
	"github.com/hollow-cube/api-server/internal/pkg/natsutil"
	"github.com/hollow-cube/api-server/internal/playerdb"
	"go.uber.org/fx"
	"go.uber.org/zap"
)

type command struct {
	Command

	handler func(h *Handler, ctx context.Context, i *Interaction) (*Response, error)
}

type HandlerParams struct {
	fx.In

	Log    *zap.SugaredLogger
	Config *config.Config

	PlayerStore *playerdb.Store
	JetStream   *natsutil.JetStreamWrapper
}

type Handler struct {
	log *zap.SugaredLogger

	playerStore *playerdb.Store
	jetStream   *natsutil.JetStreamWrapper

	Commands []*Command
	commands map[string]*command

	ladderAliases map[string]*model.PunishmentLadder
}

func NewHandler(p HandlerParams) (*Handler, error) {
	ladders, err := model.ConvertConfigLadders2Model(p.Config.PunishmentLadders)
	if err != nil {
		return nil, err
	}

	ladderAliases := make(map[string]*model.PunishmentLadder)
	for _, ladder := range ladders {
		ladderAliases[ladder.Id] = ladder
		for _, reason := range ladder.Reasons {
			ladderAliases[reason.Id] = ladder
			for _, alias := range reason.Aliases {
				ladderAliases[alias] = ladder
			}
		}
	}

	cmds := []*command{
		apiCommand,
		linkCommand,
		//recapCommand, // Disabled until command gone from mapmaker
		banCommand(ladderAliases),
	}

	pubCommands := make([]*Command, len(cmds))
	intCommands := make(map[string]*command)
	for i, cmd := range cmds {
		if len(cmd.Arguments) == 0 {
			// Always empty list, never nil
			cmd.Arguments = []Argument{}
		}

		pubCommands[i] = &cmd.Command
		intCommands[cmd.Name] = cmd
	}

	return &Handler{
		log: p.Log,

		playerStore: p.PlayerStore,
		jetStream:   p.JetStream,

		Commands:      pubCommands,
		commands:      intCommands,
		ladderAliases: ladderAliases,
	}, nil
}

func (h *Handler) ExecuteInteraction(ctx context.Context, i Interaction) (*Response, error) {
	switch i.Type {
	case TypeCommand:
		cmd, ok := h.commands[i.ID]
		if !ok {
			panic("todo unknown command")
		}
		return cmd.handler(h, ctx, &i)
	default:
		panic("todo unknown interaction type:")
	}
}
