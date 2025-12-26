package db

type PlayerSettings map[string]interface{}

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
