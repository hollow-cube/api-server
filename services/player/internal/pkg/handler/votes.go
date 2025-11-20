package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"net"
	"sync"
	"time"

	"github.com/NuVotifier/go-votifier"
	"github.com/google/uuid"
	"github.com/hollow-cube/hc-services/services/player/config"
	"github.com/hollow-cube/hc-services/services/player/internal/pkg/model"
	"github.com/hollow-cube/hc-services/services/player/internal/pkg/storage"
	"github.com/hollow-cube/hc-services/services/player/internal/pkg/util"
	"github.com/hollow-cube/hc-services/services/player/internal/pkg/wkafka"
	"go.uber.org/fx"
	"go.uber.org/zap"
)

type VoteHandler struct {
	log *zap.SugaredLogger

	listener      net.Listener
	listenerGroup *sync.WaitGroup

	producer      wkafka.SyncWriter
	storageClient storage.Client
}

type VoteHandlerParams struct {
	fx.In

	Lifecycle fx.Lifecycle

	Log    *zap.SugaredLogger
	Config *config.Config

	Producer      wkafka.SyncWriter
	StorageClient storage.Client
}

func NewVoteHandler(params VoteHandlerParams) *VoteHandler {
	h := &VoteHandler{
		log:           params.Log,
		producer:      params.Producer,
		storageClient: params.StorageClient,
	}

	token := params.Config.Votifier.Token
	if token != "" {
		params.Lifecycle.Append(fx.Hook{
			OnStart: func(ctx context.Context) (err error) {
				h.listener, err = net.Listen("tcp", params.Config.Votifier.ListenAddr)
				if err != nil {
					return err
				}

				r := []votifier.ReceiverRecord{{TokenId: votifier.StaticServiceTokenIdentifier(token)}}
				server := votifier.NewServer(h.HandleVoteReceived, r)
				go server.Serve(h.listener)
				return nil
			},
			OnStop: func(ctx context.Context) error {
				err := h.listener.Close()
				h.listenerGroup.Wait()
				return err
			},
		})
	} else {
		h.log.Info("Votifier token not set, not starting votifier server")
	}

	return h
}

func (h *VoteHandler) HandleVoteReceived(vote votifier.Vote, version votifier.VotifierProtocol, meta interface{}) {
	log := h.log.With("player", vote.Username)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var metaStr string
	if meta != nil {
		metaRaw, err := json.Marshal(meta)
		if err == nil {
			metaStr = string(metaRaw)
		}
	}

	// Find the UUID of the player from an external api (todo should probably fall back if its down, or run our own version)
	// However once found we also check to make sure we have a record for the player.
	// This is because our username cache could be out of date, so we get the uuid and test against our uuid list.
	// Do not want to process votes for players who have not joined.
	_, playerId, _, err := util.GetPlayerInfo(ctx, vote.Username)
	if err != nil {
		log.Infow("failed to lookup voting player", zap.Error(err))
		return
	}
	log = log.With("playerId", playerId)
	_, err = h.storageClient.LookupPlayerByIdOrUsername(ctx, playerId)
	if errors.Is(err, storage.ErrNotFound) {
		log.Infow("voting player has not joined the server")
		return
	} else if err != nil {
		log.Errorw("failed to lookup voting player internally", zap.Error(err))
		return
	}

	updateMessage := &model.PlayerDataUpdateMessage{
		Action: model.PlayerDataUpdate_Modify,
		Id:     playerId,
		// Other fields will be filled by computeVoteRewards
		Reason: &model.UpdateReason{
			Type:           model.UpdateReason_Vote,
			VoteSource:     vote.ServiceName,
			RelativeUpdate: &model.PlayerDataUpdateMessage{},
		},
	}

	// Create a transaction to
	// 1. Log the vote to the database for future usage
	// 2. Compute and apply the rewards
	voteId := uuid.NewString()
	err = h.storageClient.RunTransaction(ctx, func(ctx context.Context) error {
		err = h.storageClient.LogVoteEvent(ctx, voteId, time.Now(), playerId, vote.ServiceName, metaStr)
		if err != nil {
			return err
		}

		// Compute the rewards for the vote, filling the required fields in the update message
		if err = h.computeVoteRewards(ctx, voteId, updateMessage); err != nil {
			return err
		}

		return nil
	})
	if err != nil {
		log.Errorw("failed to process vote", zap.Error(err))
		return // Nothing to inform, change was not applied
	}

	sendPlayerDataUpdateMessage(h.producer, ctx, updateMessage)
	log.Info("vote processed")
}

func (h *VoteHandler) computeVoteRewards(ctx context.Context, voteId string, update *model.PlayerDataUpdateMessage) error {
	// Rewards have the following logic currently:
	// - A single reward roll is done which can result in: coins, crafting material, cubits
	// - For coins: 			Another roll is chosen for the number of coins within some bounds (below)
	// - For crafting material: Always one is given
	// - For cubits:			Same as coins, a roll is done for the number of cubits to be given (below)

	const (
		// Vote reward params
		VoteRewardChance_Cubits   = 1  // Out of 100
		VoteRewardChance_Material = 9  // Out of 100
		VoteRewardChance_Coins    = 90 // Out of 100
		VoteRewardChance_Total    = VoteRewardChance_Coins + VoteRewardChance_Material + VoteRewardChance_Cubits

		VoteRewardCoins_Min  = 50  // Inclusive
		VoteRewardCoins_Max  = 100 // Inclusive
		VoteRewardMaterial   = model.Bricks
		VoteRewardCubits_Min = 1 // Inclusive
		VoteRewardCubits_Max = 5 // Inclusive
	)

	rewardRoll := rand.Float64()
	meta := map[string]interface{}{"voteId": voteId, "roll": rewardRoll}

	// Test for Cubits reward
	x := float64(VoteRewardChance_Cubits / VoteRewardCoins_Max)
	if rewardRoll < x {
		addedCubits := rand.Intn(VoteRewardCubits_Max-VoteRewardCubits_Min+1) + VoteRewardCubits_Min
		newBalance, err := h.storageClient.AddCurrency(ctx, update.Id, model.Cubits,
			addedCubits, model.BalanceChangeReasonVote, meta)
		if err != nil {
			return fmt.Errorf("failed to give cubits vote reward: %w", err)
		}
		update.Reason.RelativeUpdate.Cubits = &addedCubits
		update.Cubits = &newBalance
		return nil
	}

	// Test for crafting material reward (assuming the player does not have too many of them)
	oldBackpack, err := h.storageClient.GetPlayerBackpack(ctx, update.Id)
	if err != nil {
		return fmt.Errorf("failed to fetch player backpack: %w", err)
	}
	x += float64(VoteRewardChance_Material / VoteRewardChance_Total)
	if oldBackpack[VoteRewardMaterial] < VoteRewardMaterial.StackSize() && rewardRoll < x {
		backpackUpdate := model.PlayerBackpack{VoteRewardMaterial: 1} // Add one brick
		newBackpack, err := h.storageClient.UpdateBackpack(ctx, update.Id, backpackUpdate)
		if err != nil {
			return fmt.Errorf("failed to give material vote reward: %w", err)
		}
		update.Reason.RelativeUpdate.Backpack = backpackUpdate
		update.Backpack = newBackpack
		return nil
	}

	// Any other case is a coins reward
	addedCoins := rand.Intn(VoteRewardCoins_Max-VoteRewardCoins_Min+1) + VoteRewardCoins_Min
	newBalance, err := h.storageClient.AddCurrency(ctx, update.Id, model.Coins,
		addedCoins, model.BalanceChangeReasonVote, meta)
	if err != nil {
		return fmt.Errorf("failed to give coins vote reward: %w", err)
	}
	update.Reason.RelativeUpdate.Coins = &addedCoins
	update.Coins = &newBalance
	return nil
}
