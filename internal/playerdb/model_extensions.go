package playerdb

import (
	"time"

	"github.com/hollow-cube/api-server/pkg/player"
)

// This is not an exhaustive list of player settings - only those used by the player service.

type PlayerSetting struct {
	Key          string
	DefaultValue interface{}
}

var (
	PlayerSettingAutoRejectFriendRequests = PlayerSetting{Key: "auto_reject_friend_requests", DefaultValue: false}
	PlayerSettingAllowDMs                 = PlayerSetting{Key: "allow_direct_messages", DefaultValue: true}
)

type PlayerSettings map[string]interface{}

func (ps PlayerSettings) GetBool(setting PlayerSetting) bool {
	value, ok := ps[setting.Key].(bool)
	if !ok {
		return setting.DefaultValue.(bool)
	}

	return value
}

type CurrencyType int

const (
	Coins CurrencyType = iota
	Cubits
)

func (c CurrencyType) String() string {
	switch c {
	case Coins:
		return "coins"
	case Cubits:
		return "cubits"
	default:
		panic("invalid currency type")
	}
}

// BalanceChangeReason is the reason for any change in a player's balance.
// The metadata about the transaction indicates more info (for example, the tebex package)
type BalanceChangeReason string

const (
	BalanceChangeReasonFaucet          BalanceChangeReason = "faucet"
	BalanceChangeReasonTebexOneoff     BalanceChangeReason = "tebex_oneoff"
	BalanceChangeReasonVote            BalanceChangeReason = "vote"
	BalanceChangeReasonBuyCosmetic     BalanceChangeReason = "buy_cosmetic"
	BalanceChangeReasonGiveItemGeneric BalanceChangeReason = "give_item_generic" // Used for map rewards, see meta
)

type PlayerSkin struct {
	Signature string `json:"signature"`
	Texture   string `json:"texture"`
}

func (pd PlayerData) EffectiveRole() player.Role {
	// Can promote default to hypercube, but otherwise pass through default
	hasHypercube := pd.HypercubeEnd != nil && pd.HypercubeEnd.After(time.Now())
	if pd.Role == player.DefaultRole && hasHypercube {
		return player.HypercubeRole
	}
	return pd.Role
}

func (pd PlayerData) Flags() player.Flags {
	return pd.EffectiveRole().Flags()
}

func (pd PlayerData) Has(flags player.Flags) bool {
	return pd.Flags().Has(flags)
}

func (pd PlayerData) TotalMapSlots() int {
	// Its kinda weird to have this map specific stuff in player service but we will merge the two later and i do NOT
	// want to deal with the distributed transaction nightmare that comes with trying to update this in map service.
	mapSlots := 2 + int(pd.ExtraMapSlots)
	if pd.Has(player.FlagExtendedLimits) {
		mapSlots += 3
	}

	return mapSlots
}

func (pd PlayerData) TotalBuilderSlots() int {
	const maxBuilderSlots = 4
	const freeBuilderSlots = 1

	builderSlots := freeBuilderSlots + int(pd.MapBuilders)
	if pd.Has(player.FlagExtendedLimits) {
		builderSlots = maxBuilderSlots
	}
	return min(builderSlots, maxBuilderSlots)
}
