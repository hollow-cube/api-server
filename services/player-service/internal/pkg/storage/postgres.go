package storage

import (
	"context"
	"errors"
	"fmt"
	"hash"
	"hash/fnv"
	"strings"
	"text/template"
	"time"

	sq "github.com/Masterminds/squirrel"
	"github.com/hollow-cube/hc-services/libraries/common/pkg/metric"
	postgresUtil "github.com/hollow-cube/hc-services/libraries/common/pkg/postgres"
	"github.com/hollow-cube/hc-services/services/player-service/internal/pkg/model"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var _ Client = &PostgresClient{}

const txContextKey = contextKey("tx")

var psql = sq.StatementBuilder.PlaceholderFormat(sq.Dollar)

var (
	playerDataColumns = []string{
		"pd.id", "pd.username", "pd.first_join", "pd.last_online", "pd.playtime",
		"pd.beta_enabled", "pd.settings", "pd.experience", "pd.coins", "pd.cubits",
	}
	playerDataScanFunc = func(p *model.PlayerData) []any {
		return []any{
			&p.Id, &p.Username, &p.FirstJoin, &p.LastOnline, &p.Playtime,
			&p.BetaEnabled, &p.Settings, &p.Experience, &p.Coins, &p.Cubits,
		}
	}
)

type PostgresClient struct {
	uri  string
	pool *pgxpool.Pool

	metrics metric.Writer

	templates map[int]*template.Template
	hash      hash.Hash32
}

func NewPostgresClient(uri string, metrics metric.Writer) (*PostgresClient, error) {
	return &PostgresClient{
		uri:     uri,
		metrics: metrics,

		templates: make(map[int]*template.Template),
		hash:      fnv.New32a(),
	}, nil
}

func NewPostgresClientFromClient(pool *pgxpool.Pool) (*PostgresClient, error) {
	c, _ := NewPostgresClient("", nil)
	c.pool = pool

	if err := c.buildSchema(context.Background()); err != nil {
		return nil, fmt.Errorf("failed to build schema: %w", err)
	}

	return c, nil
}

func (c *PostgresClient) Start(ctx context.Context) error {
	// Config options
	config, err := pgxpool.ParseConfig(c.uri)
	if err != nil {
		return fmt.Errorf("failed to parse postgres config: %w", err)
	}

	config.ConnConfig.Tracer = postgresUtil.NewSQLTracer()

	// Create pgx conn pool
	c.pool, err = pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return fmt.Errorf("failed to connect to postgres: %w", err)
	}

	if err = c.pool.Ping(ctx); err != nil {
		return fmt.Errorf("failed to ping postgres: %w", err)
	}

	return c.buildSchema(ctx)
}

func (c *PostgresClient) buildSchema(ctx context.Context) error {
	// Build backpack schema (always updated from backpack item constants)
	for _, item := range model.BackpackItems {
		query := fmt.Sprintf("alter table player_backpack add column if not exists %s int not null default 0;", item)
		if _, err := c.pool.Exec(ctx, query); err != nil {
			return fmt.Errorf("failed to add settings column: %w", err)
		}
	}

	return nil
}

func (c *PostgresClient) Shutdown(_ context.Context) error {
	c.pool.Close()
	return nil
}

func (c *PostgresClient) RunTransaction(ctx context.Context, f func(ctx context.Context) error) error {
	// If we are already in a transaction, just execute the function.
	if _, ok := ctx.Value(txContextKey).(pgx.Tx); ok {
		return f(ctx)
	}

	// Run the transaction "proper"
	tx, err := c.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("failed to start transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	ctx = context.WithValue(ctx, txContextKey, tx)
	if err := f(ctx); err != nil {
		return fmt.Errorf("failed to apply transaction: %w", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}
	return nil
}

func (c *PostgresClient) AddExperience(ctx context.Context, id string, amount int) (int, error) {
	const query = `
		update public.player_data 
		set experience = experience + $2 
		where id = $1 and experience + $2 >= 0 
		returning experience;
	`

	var newExperience int
	err := c.safeQueryRow(ctx, query, id, amount).Scan(&newExperience)
	if err != nil {
		return 0, err
	}

	go c.metrics.Write(&model.ExpChanged{
		PlayerId: id,
		Delta:    amount,
		NewValue: newExperience,
	})

	return newExperience, nil
}

var backpackSelect string

func init() {
	var query strings.Builder
	query.WriteString("select ")
	for i, item := range model.BackpackItems {
		if i != 0 {
			query.WriteString(", ")
		}
		query.WriteString(string(item))
	}
	query.WriteString(" from public.player_backpack where player_id = $1;")
	backpackSelect = query.String()
}

func (c *PostgresClient) GetPlayerBackpack(ctx context.Context, playerId string) (model.PlayerBackpack, error) {
	quantities := make([]*int, len(model.BackpackItems))
	scanEntries := make([]any, len(model.BackpackItems))
	for i := range model.BackpackItems {
		quantity := 0
		quantities[i] = &quantity
		scanEntries[i] = &quantity
	}

	err := c.safeQueryRow(ctx, backpackSelect, playerId).Scan(scanEntries...)
	if errors.Is(err, pgx.ErrNoRows) {
		return make(model.PlayerBackpack), nil
	} else if err != nil {
		return nil, err
	}

	result := make(model.PlayerBackpack, len(model.BackpackItems))
	for i, item := range model.BackpackItems {
		result[item] = *quantities[i]
	}
	return result, nil
}

func (c *PostgresClient) AddPlayerIP(ctx context.Context, playerId string, ip string) error {
	query := psql.Insert("ip_history").
		Columns("player_id", "address", "first_seen", "last_seen", "seen_count").
		Values(playerId, ip, time.Now(), time.Now(), 1).
		Suffix("on conflict (player_id, address) do update set last_seen = excluded.last_seen, seen_count = ip_history.seen_count + 1;")

	return c.safeExec3(ctx, query)
}

func (c *PostgresClient) GetPlayerIPs(ctx context.Context, playerId string) ([]string, error) {
	const query = `
		select address from public.ip_history
		where player_id = $1 order by last_seen desc;
	`

	rows, err := c.pool.Query(ctx, query, playerId)
	if errors.Is(err, pgx.ErrNoRows) {
		return []string{}, err
	} else if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]string, 0, 10)
	for rows.Next() {
		var ip string
		if err := rows.Scan(&ip); err != nil {
			return nil, err
		}
		result = append(result, ip)
	}

	return result, nil
}

func (c *PostgresClient) GetPlayersByIPs(ctx context.Context, ips []string) ([]*model.PlayerData, error) {
	query := psql.Select(playerDataColumns...).
		From("ip_history").
		InnerJoin("player_data pd ON ip_history.player_id = pd.id").
		Where(sq.Expr("address = ANY(?)", ips)).
		GroupBy("pd.id")

	return queryFunc2(ctx, c.pool, playerDataScanFunc, query)
}

func (c *PostgresClient) LookupPlayerDataBySocial(ctx context.Context, id string, platform string) (*model.PlayerData, error) {
	query := psql.Select(playerDataColumns...).
		From("player_data pd").
		RightJoin("linked_accounts la ON pd.id = la.player_id").
		Where(sq.Eq{"la.social_id": id, "la.type": platform})

	return querySingleFunc2(ctx, c.pool, playerDataScanFunc, query)
}

func (c *PostgresClient) LookupSocialByPlayerId(ctx context.Context, platform, playerId string) (string, error) {
	const query = `
		select social_id from public.linked_accounts
		where player_id = $1 and type = $2;
	`

	scanFunc := func(s *string) []any {
		return []any{s}
	}

	socialId, err := querySingleFunc(ctx, c.pool, scanFunc, query, playerId, platform)
	if err != nil {
		return "", err
	}

	return *socialId, nil
}

func (c *PostgresClient) AddLinkedAccount(ctx context.Context, playerId, socialId, platform string) error {
	const query = `
		insert into public.linked_accounts (
		    player_id, social_id, type
		) VALUES ($1, $2, $3);
	`

	return c.safeExec(ctx, query, playerId, socialId, platform)
}

func (c *PostgresClient) CreatePendingVerification(ctx context.Context, v *model.PendingVerification) error {
	const query = `
		insert into public.pending_verification (
		    type, user_id, user_secret, expiration
		) VALUES ($1, $2, $3, $4)
		on conflict (type, user_id) do update set user_secret = $3, expiration = $4;
	`

	return c.safeExec(ctx, query, v.Type, v.UserID, v.UserSecret, v.Expiration)
}

func (c *PostgresClient) DeletePendingVerification(ctx context.Context, uniqueVal *string, t model.VerificationType, isValueId bool) error {
	query := c.templateQuery(`
		delete from public.pending_verification
		where type = $1
		{{ if .IsValueId }}and user_secret = $2
		{{ else }}and user_id = $2{{ end }};
	`, struct {
		IsValueId bool
	}{isValueId})

	return c.safeExec(ctx, query, t, uniqueVal)
}

func (c *PostgresClient) GetPendingVerification(ctx context.Context, t model.VerificationType, userSecret string) (*model.PendingVerification, error) {
	const queryString = `
		select type, user_id, user_secret, expiration 
		from public.pending_verification
		where type = $1 and user_secret = $2;
	`

	scanFunc := func(v *model.PendingVerification) []any {
		return []any{&v.Type, &v.UserID, &v.UserSecret, &v.Expiration}
	}

	return querySingleFunc(ctx, c.pool, scanFunc, queryString, t, userSecret)
}

func (c *PostgresClient) SearchPlayersFuzzy(ctx context.Context, queryString string) ([]*model.PlayerData, error) {
	const query = `
		select id, username from public.player_data 
		where username ~* $1
		limit 25;
    `

	scanFunc := func(p *model.PlayerData) []any {
		return []any{&p.Id, &p.Username}
	}

	return queryFunc(ctx, c.pool, scanFunc, query, queryString)
}

func (c *PostgresClient) CreatePunishment(ctx context.Context, p *model.Punishment) error {
	const query = `
        insert into public.punishments (
            player_id, executor_id, type, created_at, ladder_id, comment, expires_at
        ) values ($1, $2, $3, $4, $5, $6, $7)
		returning id;
    `

	r := c.safeQueryRow(ctx, query, p.PlayerId, p.ExecutorId, p.Type, p.CreatedAt, p.LadderId, p.Comment, p.ExpiresAt)
	return r.Scan(&p.Id)
}

func (c *PostgresClient) GetActivePunishment(ctx context.Context, playerId string, punishmentType model.PunishmentType) (*model.Punishment, error) {
	const query = `
		select id, player_id, executor_id, type, created_at, ladder_id, comment,
		       expires_at, revoked_by, revoked_at, revoked_reason
		from public.punishments
		where player_id = $1 and type = $2 and (expires_at is null or expires_at > now()) and revoked_by is null
		order by created_at desc limit 1;
	`

	return querySingleFunc(ctx, c.pool, scanPunishment, query, playerId, punishmentType)
}

func (c *PostgresClient) SearchPunishments(
	ctx context.Context, playerId string, executorId string,
	punishmentType model.PunishmentType, ladderId string,
) ([]*model.Punishment, error) {
	const queryString = `
		SELECT id, player_id, executor_id, type, created_at, ladder_id, comment,
		       expires_at, revoked_by, revoked_at, revoked_reason
		FROM public.punishments
	`

	if playerId == "" && executorId == "" && punishmentType == "" && ladderId == "" {
		return nil, fmt.Errorf("unfiltered search")
	}

	whereBuilder := whereClauseBuilder{}
	args := make([]interface{}, 0)
	if playerId != "" {
		whereBuilder.add("player_id")
		args = append(args, playerId)
	}
	if executorId != "" {
		whereBuilder.add("executor_id")
		args = append(args, executorId)
	}
	if punishmentType != "" {
		whereBuilder.add("type")
		args = append(args, punishmentType)
	}
	if ladderId != "" {
		whereBuilder.add("ladder_id")
		args = append(args, ladderId)
	}

	query := queryString + whereBuilder.build() + ";"
	return queryFunc(ctx, c.pool, scanPunishment, query, args...)
}

func (c *PostgresClient) RevokePunishment(
	ctx context.Context, playerId string, punishmentType model.PunishmentType,
	revokedBy string, revokedReason string,
) (*model.Punishment, error) {
	const query = `
       update public.punishments
       set revoked_by = $3, revoked_at = now(), revoked_reason = $4
       where player_id = $1 and type = $2 and (expires_at is null or expires_at > now()) and revoked_by is null
       returning id, player_id, executor_id, type, created_at, ladder_id, comment,
		         expires_at, revoked_by, revoked_at, revoked_reason;
   `

	return querySingleFunc(ctx, c.pool, scanPunishment, query, playerId, punishmentType, revokedBy, revokedReason)
}

func scanPunishment(p *model.Punishment) []any {
	return []any{
		&p.Id, &p.PlayerId, &p.ExecutorId, &p.Type, &p.CreatedAt, &p.LadderId, &p.Comment,
		&p.ExpiresAt, &p.RevokedBy, &p.RevokedAt, &p.RevokedReason,
	}
}

func (c *PostgresClient) GetUnlockedCosmetics(ctx context.Context, playerId string) ([]string, error) {
	const query = `
		select cosmetic_path from public.player_cosmetics
		where player_id = $1;
	`

	rows, err := c.pool.Query(ctx, query, playerId)
	if errors.Is(err, pgx.ErrNoRows) {
		return []string{}, err
	} else if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]string, 0, 10)
	for rows.Next() {
		var cosmeticPath string
		if err := rows.Scan(&cosmeticPath); err != nil {
			return nil, err
		}
		result = append(result, cosmeticPath)
	}

	return result, nil
}

func (c *PostgresClient) UnlockCosmetic(ctx context.Context, playerId, cosmeticId string) error {
	const query = `
		insert into public.player_cosmetics (
		    player_id, cosmetic_path, unlocked_at
		) VALUES ($1, $2, $3);
	`

	return c.safeExec(ctx, query, playerId, cosmeticId, time.Now())
}

func (c *PostgresClient) AddCurrency(
	ctx context.Context, playerId string,
	currencyType model.CurrencyType, amount int,
	reason model.BalanceChangeReason, meta map[string]interface{},
) (int, error) {
	const txLogInsertQuery = `
		insert into public.tx_log (
		    player_id, timestamp, reason, currency, amount, meta
		) VALUES ($1, $2, $3, $4, $5, $6);
	`

	updateQuery := c.templateQuery(`
		update public.player_data 
		set {{ .Field }} = {{ .Field }} + $2 
		where id = $1 and {{ .Field }} + $2 >= 0 
		returning {{ .Field }};
	`, struct {
		Field string
	}{model.CurrencyType.String(currencyType)})

	var newBalance int
	err := c.RunTransaction(ctx, func(ctx context.Context) error {
		// Insert into tx_log
		if err := c.safeExec(ctx, txLogInsertQuery, playerId, time.Now(), reason, currencyType.String(), amount, meta); err != nil {
			return fmt.Errorf("failed to insert into tx_log: %w", err)
		}

		// Update player balance
		err := c.safeQueryRow(ctx, updateQuery, playerId, amount).Scan(&newBalance)
		if err != nil {
			return fmt.Errorf("failed to update player balance: %w", err)
		}

		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("failed to add currency: %w", err)
	}

	go func() {
		switch currencyType {
		case model.Coins:
			c.metrics.Write(&model.CoinBalanceChanged{
				PlayerId: playerId,
				Delta:    amount,
				NewValue: newBalance,
			})
		case model.Cubits:
			c.metrics.Write(&model.CubitBalanceChanged{
				PlayerId: playerId,
				Delta:    amount,
				NewValue: newBalance,
			})
		}
	}()

	return newBalance, nil
}

func (c *PostgresClient) GetPlayerRecapById(ctx context.Context, id string) (*model.Recap, error) {
	query := psql.Select("player_recaps.id", "player_data.id", "player_data.username", "player_recaps.year", "player_recaps.data").
		From("player_recaps").
		Join("player_data ON player_recaps.player_id = player_data.id").
		Where(sq.Expr("player_recaps.id = $1", id))

	scanFunc := func(v *model.Recap) []any {
		return []any{&v.Id, &v.PlayerId, &v.Username, &v.Year, &v.Data}
	}

	return querySingleFunc2(ctx, c.pool, scanFunc, query)
}

func (c *PostgresClient) GetPlayerRecapByPlayer(ctx context.Context, playerId string, year int) (*model.Recap, error) {
	query := psql.Select("player_recaps.id", "player_data.id", "player_data.username", "player_recaps.year", "player_recaps.data").
		From("player_recaps").
		Join("player_data ON player_recaps.player_id = player_data.id").
		Where(sq.Expr("player_id = $1 AND year = $2", playerId, year))

	scanFunc := func(v *model.Recap) []any {
		return []any{&v.Id, &v.PlayerId, &v.Username, &v.Year, &v.Data}
	}

	return querySingleFunc2(ctx, c.pool, scanFunc, query)
}

func (c *PostgresClient) CreatePendingTransaction(ctx context.Context, checkoutId, playerId, username string) error {
	const query = `
		insert into public.pending_tebex_transactions (
		    player_id, created_at, username, checkout_id
		) VALUES ($1, $2, $3, $4);
    `

	return c.safeExec(ctx, query, playerId, time.Now(), username, checkoutId)
}

func (c *PostgresClient) GetPendingTransaction(ctx context.Context, checkoutId string) (basketId *string, username string, err error) {
	const query = `
		select basket_id, username from public.pending_tebex_transactions
		where checkout_id = $1;
	`

	err = c.safeQueryRow(ctx, query, checkoutId).Scan(&basketId, &username)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, "", ErrNotFound
	}
	return
}

func (c *PostgresClient) ResolvePendingTransaction(ctx context.Context, checkoutId, basketId string) error {
	const query = `
		update public.pending_tebex_transactions
		set basket_id = $2
		where checkout_id = $1;
	`

	tag, err := c.safeExec2(ctx, query, checkoutId, basketId)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}

	return nil
}

func (c *PostgresClient) CreateTebexState(ctx context.Context, txId string, changes []*model.TebexChange) error {
	const query = `
		insert into public.tebex_state (
		    tx_id, changes
		) values ($1, $2)
		on conflict do nothing;
	`

	tag, err := c.safeExec2(ctx, query, txId, changes)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		// Uh oh ID already exists so no rows were changed
		return ErrDuplicateEntry
	}

	return nil
}

func (c *PostgresClient) RevertTebexState(ctx context.Context, txId string) error {
	const query1 = `
		update public.tebex_state
		set reverted = true
		where tx_id = $1 and not reverted;
	`

	tag, err := c.safeExec2(ctx, query1, txId)
	if err != nil || tag.RowsAffected() > 0 {
		// This is kinda cursed but either its an error and we return that, or we affected a row
		// and so this is a success and we return nil (because err was nil)
		return err
	}

	// Otherwise, we need to differentiate between the transaction not existing and it already being reverted.
	const query2 = `
		select tx_id from public.tebex_state
		where tx_id = $1;
	`

	var result string
	err = c.safeQueryRow(ctx, query2, txId).Scan(&result)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}

	// If we got here then the transaction exists but is already reverted.
	return nil
}

func (c *PostgresClient) LogTebexEvent(ctx context.Context, id string, time time.Time, raw string) error {
	const query = `
		insert into public.tebex_events (
		    event_id, timestamp, raw
		) VALUES ($1, $2, $3);
	`
	return c.safeExec(ctx, query, id, time, raw)
}

func (c *PostgresClient) LogVoteEvent(ctx context.Context, id string, time time.Time, playerId, source, meta string) error {
	const query = `
		insert into public.vote_events (
		    vote_id, player_id, timestamp, source, meta
		) VALUES ($1, $2, $3, $4, $5);
	`
	return c.safeExec(ctx, query, id, playerId, time, source, meta)
}

func (c *PostgresClient) safeExec(ctx context.Context, query string, args ...interface{}) error {
	_, err := c.safeExec2(ctx, query, args...)
	return err
}

func (c *PostgresClient) safeExec3(ctx context.Context, query sq.Sqlizer) error {
	sql, args, err := query.ToSql()
	if err != nil {
		return err
	}

	_, err = c.safeExec2(ctx, sql, args...)
	return err
}

func (c *PostgresClient) safeExec2(ctx context.Context, query string, args ...interface{}) (pgconn.CommandTag, error) {
	// Execute in transaction if it is running in the current context
	if tx, ok := ctx.Value(txContextKey).(pgx.Tx); ok {
		return tx.Exec(ctx, query, args...)
	}

	// Otherwise execute as normal
	return c.pool.Exec(ctx, query, args...)
}

func (c *PostgresClient) safeQueryRow(ctx context.Context, query string, args ...interface{}) pgx.Row {
	// Execute in transaction if it is running in the current context
	if tx, ok := ctx.Value(txContextKey).(pgx.Tx); ok {
		return tx.QueryRow(ctx, query, args...)
	}

	// Otherwise execute as normal
	return c.pool.QueryRow(ctx, query, args...)
}

func (c *PostgresClient) do(ctx context.Context, query sq.Sqlizer, scanRows ...any) (row pgx.Row, err error) {
	q, args, err := query.ToSql()
	if err != nil {
		return nil, err
	}

	if len(scanRows) > 0 {
		if tx, ok := ctx.Value(txContextKey).(pgx.Tx); ok {
			row = tx.QueryRow(ctx, q, args...)
		} else {
			row = c.pool.QueryRow(ctx, q, args...)
		}
		if err = row.Scan(scanRows...); errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return row, err
	} else {
		if tx, ok := ctx.Value(txContextKey).(pgx.Tx); ok {
			_, err = tx.Exec(ctx, q, args...)
		} else {
			_, err = c.pool.Exec(ctx, q, args...)
		}
		return nil, err
	}
}
