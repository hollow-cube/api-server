package storage

//go:generate mockgen -source=storage.go -destination=mock_storage/storage.gen.go

import (
	"context"
	"errors"
	"time"

	"github.com/hollow-cube/hc-services/services/player-service/internal/pkg/model"
	"github.com/hollow-cube/hc-services/services/player-service/internal/pkg/totp"
)

var (
	ErrNotFound        = errors.New("not found")
	ErrDuplicateEntry  = errors.New("duplicate entry")
	ErrAlreadyReverted = errors.New("already reverted")
	ErrBalanceTooLow   = errors.New("balance too low")
)

type contextKey string

type Client interface {
	RunTransaction(ctx context.Context, f func(ctx context.Context) error) error

	CountPlayerStats(ctx context.Context) (int, int, error)
	CreatePlayerData(ctx context.Context, p *model.PlayerData) error
	GetPlayerData(ctx context.Context, id string) (*model.PlayerData, error)
	UpdatePlayerData(ctx context.Context, p *model.PlayerData) error
	AddExperience(ctx context.Context, id string, amount int) (int, error)
	GetPlayerBackpack(ctx context.Context, playerId string) (model.PlayerBackpack, error)

	AddPlayerIP(ctx context.Context, playerId string, ip string) error
	// GetPlayerIPs returns the IP addresses used by the player ordered from most recent to oldest.
	GetPlayerIPs(ctx context.Context, playerId string) ([]string, error)
	GetPlayersByIPs(ctx context.Context, ips []string) ([]*model.PlayerData, error)

	// TOTP
	GetTOTP(ctx context.Context, playerId string) (*totp.Config, error)
	AddTOTP(ctx context.Context, config *totp.Config) (bool, error)
	ActivateTOTP(ctx context.Context, playerId string, key []byte) error
	DeleteTOTP(ctx context.Context, playerId string) error

	AddLinkedAccount(ctx context.Context, playerId, socialId, platform string) error

	LookupPlayerDataBySocial(ctx context.Context, id string, platform string) (*model.PlayerData, error)
	LookupSocialByPlayerId(ctx context.Context, platform, playerId string) (string, error)

	CreatePendingVerification(ctx context.Context, v *model.PendingVerification) error
	GetPendingVerification(ctx context.Context, t model.VerificationType, userSecret string) (*model.PendingVerification, error)
	DeletePendingVerification(ctx context.Context, uniqueVal *string, t model.VerificationType, isValueId bool) error

	// SearchPlayersFuzzy searches the player data collection for a fuzzy match of the given query string.
	// The returned objects ONLY have the ID and username fields.
	// Currently has a hardcoded limit of 25 entries.
	SearchPlayersFuzzy(ctx context.Context, query string) ([]*model.PlayerData, error)

	// CreatePunishment writes the given punishment to the database, setting p.Id to the newly created ID.
	CreatePunishment(ctx context.Context, p *model.Punishment) error
	GetActivePunishment(ctx context.Context, playerId string, punishmentType model.PunishmentType) (*model.Punishment, error)
	SearchPunishments(
		ctx context.Context, playerId string, executorId string,
		punishmentType model.PunishmentType, ladderId string,
	) ([]*model.Punishment, error)
	RevokePunishment(ctx context.Context, playerId string, punishmentType model.PunishmentType, revokedBy string, revokedReason string) (*model.Punishment, error)

	// Cosmetics
	GetUnlockedCosmetics(ctx context.Context, playerId string) ([]string, error)
	UnlockCosmetic(ctx context.Context, playerId, cosmeticId string) error

	// Currency related methods
	// The following methods are the ONLY way that currency values for a player should be altered.
	// TODO: This should maybe be moved out of storage client, but it is heavily dependent on the backing storage
	//       to handle things like transactions.

	// AddCurrency adds the given amount of the given currency type to the player's balance.
	// It is valid to call
	//
	// Returns the new balance of the player.
	AddCurrency(
		ctx context.Context, playerId string,
		currencyType model.CurrencyType, amount int,
		reason model.BalanceChangeReason, meta map[string]interface{},
	) (int, error)
	// UpdateBackpack adds the relative values from the given backpack if they are nonzero, otherwise ignores.
	// It is valid to have negative values present, however the caller should validate the update.
	//
	// todo: this makes no sense. The caller cannot do that validation transactionally. it needs to be enforced by the query in a txn.
	//
	// Returns a backpack with the current value of each material ONLY if it was present in the initial change.
	UpdateBackpack(ctx context.Context, playerId string, relativeBackpack model.PlayerBackpack) (model.PlayerBackpack, error)

	// Misc methods

	// CreatePendingTransaction records a new pending transaction in the database.
	CreatePendingTransaction(ctx context.Context, checkoutId, playerId, username string) error
	GetPendingTransaction(ctx context.Context, checkoutId string) (*string, string, error)
	ResolvePendingTransaction(ctx context.Context, checkoutId, basketId string) error

	// CreateTebexState attempts to write the given change list to the database as a new tebex transaction.
	// Returns ErrDuplicateEntry if the transaction has already been recorded.
	// In that case, the transaction should be aborted successfully. This turns transaction writing into an idempotent operation.
	CreateTebexState(ctx context.Context, txId string, changes []*model.TebexChange) error
	// RevertTebexState attempts to mark the given transaction as reverted.
	// Returns ErrNotFound if the transaction does not exist, and ErrAlreadyReverted if it has been reverted.
	// The transaction not existing is a problem, however it being reverted already should result in a successful processing
	// of the transaction (even though it will be aborted). This is to make the operation idempotent.
	RevertTebexState(ctx context.Context, txId string) error

	// LogTebexEvent logs the given Tebex event to the database as is.
	// This is used to store a copy of the raw event received for debugging/historical purposes.
	//
	// This method is generally used in a transaction with AddCurrency/etc.
	LogTebexEvent(ctx context.Context, id string, time time.Time, raw string) error
	LogVoteEvent(ctx context.Context, id string, time time.Time, playerId, source, meta string) error

	GetPlayerRecapById(ctx context.Context, id string) (*model.Recap, error)
	GetPlayerRecapByPlayer(ctx context.Context, playerId string, year int) (*model.Recap, error)
}
