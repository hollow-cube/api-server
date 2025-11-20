package model

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

type TebexChangeType int

const (
	TebexChangeCubits TebexChangeType = iota
	TebexChangeHypercube
)

type TebexChange struct {
	Target string          `json:"target"` // The player's id
	Type   TebexChangeType `json:"type"`
	Amount int             `json:"amount"` // Cubits or Hypercube duration in months
}

type Rarity int

const (
	Common Rarity = 1 << iota
	Rare
	Epic
	Legendary
)

func (r Rarity) StackSize() int {
	return 32 / int(r)
}

type BackpackItem string

func (i BackpackItem) Rarity() Rarity {
	return backpackItemRarityMap[i]
}

func (i BackpackItem) StackSize() int {
	return i.Rarity().StackSize()
}

const (
	// Crafting Materials
	Bricks        BackpackItem = "bricks"
	Cloth         BackpackItem = "cloth"
	Gem           BackpackItem = "gem"
	Goo           BackpackItem = "goo"
	Metal         BackpackItem = "metal"
	String        BackpackItem = "string"
	BoneFragment  BackpackItem = "bone_fragment"
	Controller    BackpackItem = "controller"
	FlowerPetal   BackpackItem = "flower_petal"
	SugarCube     BackpackItem = "sugar_cube"
	FireworkDust  BackpackItem = "firework_dust"
	GoldChunk     BackpackItem = "gold_chunk"
	InfernalFlame BackpackItem = "infernal_flame"
	DragonScale   BackpackItem = "dragon_scale"
	NightmareFuel BackpackItem = "nightmare_fuel"
	Stardust      BackpackItem = "stardust"

	// Dyes
	RedDye       BackpackItem = "red_dye"
	OrangeDye    BackpackItem = "orange_dye"
	YellowDye    BackpackItem = "yellow_dye"
	LimeDye      BackpackItem = "lime_dye"
	GreenDye     BackpackItem = "green_dye"
	TealDye      BackpackItem = "teal_dye"
	CyanDye      BackpackItem = "cyan_dye"
	BlueDye      BackpackItem = "blue_dye"
	PurpleDye    BackpackItem = "purple_dye"
	MagentaDye   BackpackItem = "magenta_dye"
	GreyscaleDye BackpackItem = "greyscale_dye"
)

var backpackItemRarityMap = map[BackpackItem]Rarity{
	Bricks: Common, Cloth: Common, Gem: Common, Goo: Common, Metal: Common, String: Common,
	BoneFragment: Rare, Controller: Rare, FlowerPetal: Rare, SugarCube: Rare,
	FireworkDust: Epic, GoldChunk: Epic, InfernalFlame: Epic,
	DragonScale: Legendary, NightmareFuel: Legendary, Stardust: Legendary,

	//todo rarities
	RedDye:       Common,
	OrangeDye:    Common,
	YellowDye:    Common,
	LimeDye:      Common,
	GreenDye:     Common,
	TealDye:      Common,
	CyanDye:      Common,
	BlueDye:      Common,
	PurpleDye:    Common,
	MagentaDye:   Common,
	GreyscaleDye: Common,
}

var BackpackItems []BackpackItem

type PlayerBackpack map[BackpackItem]int

func init() {
	for i := range backpackItemRarityMap {
		BackpackItems = append(BackpackItems, i)
	}
}
