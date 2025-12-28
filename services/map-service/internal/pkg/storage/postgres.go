package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"hash/fnv"
	"math/rand"
	"regexp"
	"strings"
	"text/template"
	"time"

	sq "github.com/Masterminds/squirrel"
	postgresUtil "github.com/hollow-cube/hc-services/libraries/common/pkg/postgres"
	"github.com/hollow-cube/hc-services/services/map-service/internal/pkg/model"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"go.uber.org/zap"
)

var _ Client = &PostgresClient{}

var psql = sq.StatementBuilder.PlaceholderFormat(sq.Dollar)

const txContextKey = contextKey("tx")

type PostgresClient struct {
	log *zap.SugaredLogger

	uri  string
	pool *pgxpool.Pool

	templates map[int]*template.Template
	hash      hash.Hash32
}

func NewPostgresClient(uri string) (*PostgresClient, error) {
	return &PostgresClient{
		log:       zap.S(),
		uri:       uri,
		templates: make(map[int]*template.Template),
		hash:      fnv.New32a(),
	}, nil
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
	return nil
}

func (c *PostgresClient) Shutdown(_ context.Context) error {
	c.pool.Close()
	return nil
}

func (c *PostgresClient) RunTransaction(ctx context.Context, f func(ctx context.Context) error) error {
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

func (c *PostgresClient) CreateMap(ctx context.Context, m *model.Map) error {
	const query = `
		insert into public.maps (
			id, owner, m_type, created_at, updated_at, authz_key, 
			file_id, legacy_map_id, published_id, published_at, 
			opt_name, opt_icon, size, opt_variant, opt_subvariant, 
			opt_spawn_point, protocol_version, contest
		) values (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18
		);
	`

	// Spawn point to string
	spawnPoint, err := json.Marshal(m.Settings.SpawnPoint)
	if err != nil {
		return fmt.Errorf("failed to marshal spawn point: %w", err)
	}

	return c.safeExec(ctx, query,
		m.Id, m.Owner, m.Type, m.CreatedAt, m.UpdatedAt, m.AuthzKey,
		m.MapFileId, m.LegacyMapId, m.PublishedId, m.PublishedAt,
		m.Settings.Name, m.Settings.Icon, m.Settings.Size,
		m.Settings.Variant, m.Settings.SubVariant,
		string(spawnPoint), m.ProtocolVersion, m.Contest,
	)
}

func (c *PostgresClient) GetMapById(ctx context.Context, id string) (*model.Map, error) {
	maps, err := c.GetMapsByIds(ctx, []string{id})
	if err != nil {
		return nil, err
	}
	if len(maps) == 0 {
		return nil, ErrNotFound
	}
	return maps[0], nil
}

func (c *PostgresClient) GetMapsByIds(ctx context.Context, ids []string) ([]*model.Map, error) {
	const query = `
		select
			m.id, m.owner, m.m_type, m.created_at, m.updated_at, m.authz_key, m.verification,
			m.file_id, m.legacy_map_id, m.published_id, m.published_at, m.quality_override,
			m.opt_name, m.opt_icon, m.size, m.opt_variant, m.opt_subvariant, m.opt_spawn_point,
		
			m.opt_only_sprint, m.opt_no_sprint, m.opt_no_jump, m.opt_no_sneak, m.opt_boat, m.opt_extra, 
			m.opt_tags, m.protocol_version, m.contest, m.listed,
		
			m.ext,
			coalesce(stats.play_count, 0) AS play_count,
			coalesce(stats.win_count, 0)   as win_count,
			coalesce(likes.total_likes, 0) as total_likes
		from public.maps as m
		LEFT JOIN (SELECT map_id, play_count, win_count
			FROM map_stats
			GROUP BY map_id
		) stats ON m.id = stats.map_id
		LEFT JOIN (
			SELECT map_id, SUM(CASE WHEN rating = 1 THEN 1 WHEN rating = 2 THEN -1 ELSE 0 END) AS total_likes
			FROM map_ratings
			GROUP BY map_id
		) likes ON m.id = likes.map_id
		where m.deleted_at is null and id = ANY($1);
	`

	rows, err := c.pool.Query(ctx, query, ids)

	if err != nil {
		return nil, err
	}

	var maps []*model.Map
	for rows.Next() {
		m, err := c.scanMap(rows)
		if err != nil {
			return nil, err
		}
		maps = append(maps, m)
	}

	return maps, err
}

func (c *PostgresClient) GetMapByPublishedId(ctx context.Context, id string) (*model.Map, error) {
	const query = `
		select
			m.id, m.owner, m.m_type, m.created_at, m.updated_at, m.authz_key, m.verification,
			m.file_id, m.legacy_map_id, m.published_id, m.published_at, m.quality_override,
			m.opt_name, m.opt_icon, m.size, m.opt_variant, m.opt_subvariant, m.opt_spawn_point,
		
			m.opt_only_sprint, m.opt_no_sprint, m.opt_no_jump, m.opt_no_sneak, m.opt_boat, m.opt_extra, 
			m.opt_tags, m.protocol_version, m.contest, m.listed,
		
			m.ext,
			coalesce(stats.play_count, 0) AS play_count,
			coalesce(stats.win_count, 0)   as win_count,
			coalesce(likes.total_likes, 0) as total_likes
		from public.maps as m
		LEFT JOIN (SELECT map_id, play_count, win_count
			FROM map_stats
			GROUP BY map_id
		) stats ON m.id = stats.map_id
		LEFT JOIN (
			SELECT map_id, SUM(CASE WHEN rating = 1 THEN 1 WHEN rating = 2 THEN -1 ELSE 0 END) AS total_likes
			FROM map_ratings
			GROUP BY map_id
		) likes ON m.id = likes.map_id
		where m.deleted_at is null and m.published_id = $1;
	`

	r := c.pool.QueryRow(ctx, query, id)
	return c.scanMap(r)
}

func (c *PostgresClient) scanMap(r pgx.Row) (*model.Map, error) {
	var spawnPoint string
	var rawExt []byte
	var rawExtra []byte
	var m model.Map
	var uniqueWins int
	err := r.Scan(
		&m.Id, &m.Owner, &m.Type, &m.CreatedAt, &m.UpdatedAt, &m.AuthzKey, &m.Verification,
		&m.MapFileId, &m.LegacyMapId, &m.PublishedId, &m.PublishedAt, &m.QualityOverride,
		&m.Settings.Name, &m.Settings.Icon, &m.Settings.Size, &m.Settings.Variant, &m.Settings.SubVariant, &spawnPoint,
		&m.Settings.OnlySprint, &m.Settings.NoSprint, &m.Settings.NoJump, &m.Settings.NoSneak, &m.Settings.Boat, &rawExtra,
		&m.Settings.Tags, &m.ProtocolVersion, &m.Contest, &m.Listed, &rawExt, &m.UniquePlays, &uniqueWins, &m.Likes,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to fetch map: %w", err)
	}

	if spawnPoint != "" {
		if err := json.Unmarshal([]byte(spawnPoint), &m.Settings.SpawnPoint); err != nil {
			return nil, fmt.Errorf("failed to unmarshal spawn point: %w", err)
		}
	}

	if m.UniquePlays > 0 {
		m.ClearRate = float64(uniqueWins) / float64(m.UniquePlays)
	}

	if err := json.Unmarshal(rawExt, &m.MapExt); err != nil {
		return nil, fmt.Errorf("failed to unmarshal map ext: %w", err)
	}
	if len(rawExtra) > 0 {
		if err := json.Unmarshal(rawExtra, &m.Settings.Extra); err != nil {
			return nil, fmt.Errorf("failed to unmarshal extra: %w", err)
		}
	}

	return &m, nil
}

func (c *PostgresClient) UpdateMap(ctx context.Context, m *model.Map) error {
	const query = `
		update public.maps set 
		    updated_at=$2, authz_key=$3, file_id=$4, published_id=$5, published_at=$6, verification=$7,
		    opt_name=$8, opt_icon=$9, size=$10, opt_variant=$11, opt_subvariant=$12, opt_spawn_point=$13,
		    opt_only_sprint=$14, opt_no_sprint=$15, opt_no_jump=$16, opt_no_sneak=$17, opt_boat=$18, 
		    opt_extra=$19, opt_tags=$20, ext=$21, quality_override=$22, protocol_version=$23, contest=$24, listed=$25
		where deleted_at is null and id = $1;
	`

	// Spawn point to string
	spawnPoint, err := json.Marshal(m.Settings.SpawnPoint)
	if err != nil {
		return fmt.Errorf("failed to marshal spawn point: %w", err)
	}

	// MapExt to string
	ext, err := json.Marshal(m.MapExt)
	if err != nil {
		return fmt.Errorf("failed to marshal map ext: %w", err)
	}

	var extra []byte
	if len(m.Settings.Extra) > 0 {
		extra, err = json.Marshal(m.Settings.Extra)
		if err != nil {
			return fmt.Errorf("failed to marshal extra: %w", err)
		}
	}

	zap.S().Infow("updating map", "id", m.Id, "extra", m.Settings.Extra)
	return c.safeExec(ctx, query,
		m.Id, m.UpdatedAt, m.AuthzKey, m.MapFileId, m.PublishedId, m.PublishedAt,
		m.Verification, m.Settings.Name, m.Settings.Icon, m.Settings.Size,
		m.Settings.Variant, m.Settings.SubVariant, string(spawnPoint),
		m.Settings.OnlySprint, m.Settings.NoSprint, m.Settings.NoJump, m.Settings.NoSneak, m.Settings.Boat, extra,
		m.Settings.Tags, ext, m.QualityOverride, m.ProtocolVersion, m.Contest, m.Listed,
	)
}

func (c *PostgresClient) DeleteMapSoft(ctx context.Context, mapId, playerId, deleteReason string) error {
	return c.RunTransaction(ctx, func(ctx context.Context) error {
		const deleteSaveStatesQuery = `update public.save_states set deleted = now() where map_id = $1;`
		if err := c.safeExec(ctx, deleteSaveStatesQuery, mapId); err != nil {
			return fmt.Errorf("delete save states step failed: %w", err)
		}

		const deleteMapQuery = `update public.maps set deleted_at = now(), deleted_by=$2, deleted_reason=$3 where id=$1;`
		if err := c.safeExec(ctx, deleteMapQuery, mapId, playerId, deleteReason); err != nil {
			return fmt.Errorf("delete map step failed: %w", err)
		}

		return nil
	})
}

func (c *PostgresClient) SearchOrgMaps(ctx context.Context, page, pageSize int, orgId string) ([]*model.Map, bool, error) {
	const query = `
		select 
		    id, owner, m_type, created_at, updated_at, authz_key, verification,
			file_id, legacy_map_id, published_id, published_at, quality_override,
			opt_name, opt_icon, size, opt_variant, opt_subvariant, opt_spawn_point,
			
			opt_only_sprint, opt_no_sprint, opt_no_jump, opt_no_sneak, opt_boat, opt_extra, 
			opt_tags, protocol_version, contest, listed
			
			ext,
			0 as play_count,
			0 as win_count,
			0 as total_likes
		from public.maps 
		where deleted_at is null and m_type = 'org' and owner = $3
		order by updated_at desc
		limit $1 offset $2;
	`

	r, err := c.pool.Query(ctx, query, pageSize+1, page*pageSize, orgId)
	if err != nil {
		return nil, false, fmt.Errorf("failed to query maps: %w", err)
	}
	defer r.Close()

	result := make([]*model.Map, 0, pageSize)
	for i := 0; i < pageSize && r.Next(); i++ {
		m, err := c.scanMap(r)
		if err != nil {
			return nil, false, fmt.Errorf("failed to read row: %w", err)
		}

		result = append(result, m)
	}

	zap.S().Infow("searching org maps", "page", page, "pageSize", pageSize, "orgId", orgId, "resultCount", len(result))

	return result, r.Next(), nil
}

var (
	difficultySelect = strings.NewReplacer("\n", " ", "\t", "").Replace(`
		CASE
			WHEN play_count < 10 THEN -1
			WHEN clear_rate < 0.05 THEN 4
			WHEN clear_rate < 0.25 THEN 3
			WHEN clear_rate < 0.5 THEN 2
			WHEN clear_rate < 0.75 THEN 1
			ELSE 0
		END AS difficulty
	`)
	fullMapColumns = []string{
		"m.id", "m.owner", "m.m_type", "m.created_at", "m.updated_at",
		"m.published_id", "m.published_at", "m.quality_override", "m.opt_name",
		"m.size", "m.opt_icon", "m.opt_variant", "m.opt_subvariant", "m.opt_only_sprint",
		"m.opt_no_sprint", "m.opt_no_jump", "m.opt_no_sneak", "m.opt_boat", "m.opt_tags",
		"m.protocol_version", "m.contest", "m.listed",
		"COALESCE(stats.play_count, 0) AS play_count", "COALESCE(stats.clear_rate, 0) AS clear_rate",
		"GREATEST(COALESCE(likes.total_likes, 0), 0) AS likes", "COALESCE(stats.difficulty, -1) as difficulty",
	}
	fullMapColumnScan = func(m *model.Map) []any {
		return []any{&m.Id, &m.Owner, &m.Type, &m.CreatedAt, &m.UpdatedAt,
			&m.PublishedId, &m.PublishedAt, &m.QualityOverride, &m.Settings.Name,
			&m.Settings.Size, &m.Settings.Icon, &m.Settings.Variant, &m.Settings.SubVariant, &m.Settings.OnlySprint,
			&m.Settings.NoSprint, &m.Settings.NoJump, &m.Settings.NoSneak, &m.Settings.Boat, &m.Settings.Tags,
			&m.ProtocolVersion, &m.Contest, &m.Listed, &m.UniquePlays, &m.ClearRate, &m.Likes, &m.Difficulty2}
	}
	mapStatsSubquery = mustCompile(psql.Select("map_id", "play_count", "win_count", "clear_rate", difficultySelect).
				From("public.map_stats").GroupBy("map_id"))
	mapLikesSubquery = mustCompile(psql.Select("map_id", "SUM(CASE WHEN rating = 1 THEN 1 WHEN rating = 2 THEN -1 ELSE 0 END) AS total_likes").
				From("public.map_ratings").GroupBy("map_id"))
)

func (c *PostgresClient) SearchMapsV3(ctx context.Context, params SearchQueryV3) (m []*model.Map, err error) {
	query := psql.Select(fullMapColumns...).From("public.maps m").
		LeftJoin("(" + mapStatsSubquery + ") stats ON m.id = stats.map_id").
		LeftJoin("(" + mapLikesSubquery + ") likes ON m.id = likes.map_id")
	query = buildSearchV3Query(query, params, true, true)

	results, err := doMultiScan(c.pool, ctx, query, fullMapColumnScan)
	if errors.Is(err, ErrNotFound) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}

	return results, nil
}

func (c *PostgresClient) SearchMapsCountV3(ctx context.Context, params SearchQueryV3) (count int, err error) {
	query := psql.Select("count(*) as count").From("public.maps m").
		LeftJoin("(" + mapStatsSubquery + ") stats ON m.id = stats.map_id")
	query = buildSearchV3Query(query, params, false, false)
	_, err = c.do(ctx, query, &count)
	return
}

func buildSearchV3Query(query sq.SelectBuilder, params SearchQueryV3, sort, limit bool) sq.SelectBuilder {
	filters := sq.And{
		sq.Eq{"deleted_at": nil},
		sq.NotEq{"published_id": nil},
		sq.Eq{"opt_variant": params.Variant},
		sq.Eq{"listed": true},
	}

	if params.Owner != "" {
		filters = append(filters, sq.Eq{"owner": params.Owner})
	}
	if params.Query != "" {
		filters = append(filters, sq.Expr("opt_name ~* ?", regexp.QuoteMeta(params.Query)))
	}
	if params.Contest != "" {
		filters = append(filters, sq.Eq{"contest": params.Contest})
	}

	if len(params.Quality) > 0 {
		filters = append(filters, sq.Eq{"quality_override": params.Quality})
	}
	if len(params.Difficulty) > 0 {
		filters = append(filters, sq.Eq{"stats.difficulty": params.Difficulty})
	}

	query = query.Where(filters)

	if sort {
		sortOrder := "desc"
		if params.SortOrder != MapSortDesc {
			sortOrder = "asc"
		}
		if params.Sort == MapSortBest {
			query = query.OrderBy("m.quality_override "+sortOrder, "likes.total_likes desc", "m.published_at desc")
		} else if params.Sort == MapSortRandom {
			query = query.OrderBy("random()")
		} else { // Default is MapSortPublished
			query = query.OrderBy("m.published_at " + sortOrder)
		}
	}

	if limit {
		query = query.Offset(uint64(params.Page * params.PageSize)).
			Limit(uint64(params.PageSize))
	}

	return query
}

func (c *PostgresClient) GetMapProgress(ctx context.Context, playerId string, mapIds []string) ([]*model.MapIdAndProgress, error) {
	const queryString = `
		WITH ranked_save_states AS (
			SELECT
				m.id AS map_id,
				ss.completed::int AS completed,
				ss.playtime,
				ss.updated
			FROM 
				(SELECT unnest($1::uuid[]) AS id) m
			LEFT JOIN 
				save_states ss 
			ON 
				ss.map_id = m.id AND ss.player_id = $2
			WHERE 
				ss.deleted IS NULL 
				AND (ss.type = 'playing' OR ss.type = 'verifying')
		),
		progress_and_playtime AS (
			SELECT
				map_id,
				COALESCE(MAX(completed), 0) AS progress,
				CASE
					WHEN MAX(completed) = 1 THEN MIN(playtime) FILTER (WHERE completed = 1)
					ELSE (
						SELECT playtime
						FROM ranked_save_states rss
						WHERE rss.map_id = rs.map_id
						ORDER BY updated DESC
						LIMIT 1
					)
				END AS playtime
			FROM 
				ranked_save_states rs
			GROUP BY 
				map_id
		)
		SELECT 
			map_id,
			progress + 1 AS progress,
			playtime
		FROM 
			progress_and_playtime;
	`

	scanFunc := func(m *model.MapIdAndProgress) []any {
		return []any{&m.MapId, &m.Progress, &m.Playtime}
	}

	return queryFunc[model.MapIdAndProgress](ctx, c.pool, scanFunc, queryString, mapIds, playerId)
}

func (c *PostgresClient) FindNextPublishedId(ctx context.Context) (int64, error) {
	const query = `select from public.maps where published_id=$1 limit 1;`

	var id int64
	for i := 0; i < 10; i++ {
		id = rand.Int63n(MaxPublishedMapId-1) + 1 // We do not want zero as an Id
		err := c.pool.QueryRow(ctx, query, id).Scan()
		if errors.Is(err, pgx.ErrNoRows) {
			// Found a free Id, return it.
			return id, nil
		}
		if err != nil {
			return 0, fmt.Errorf("failed to check published id: %w", err)
		}
	}

	return 0, fmt.Errorf("failed to create new published id in 10 attempts")
}

func (c *PostgresClient) GetRecentMaps(ctx context.Context, page, pageSize int, playerId string, saveStateType model.SaveStateType) ([]*model.Map, bool, error) {
	query := psql.Select("map_id").From("public.save_states").
		Where(sq.Eq{"player_id": playerId, "type": saveStateType, "deleted": nil}).
		GroupBy("map_id").OrderBy("max(updated) desc").
		Offset(uint64(page * pageSize)).Limit(uint64(pageSize + 1))
	rows, err := c.doMulti(ctx, query)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()

	var maps []*model.Map
	for rows.Next() {
		m := &model.Map{}
		if err = rows.Scan(&m.Id); err != nil {
			return nil, false, err
		}
		maps = append(maps, m)
	}

	if len(maps) > pageSize {
		return maps[0:pageSize], true, nil
	}
	return maps, false, nil
}

func (c *PostgresClient) WriteReport(ctx context.Context, report *model.MapReport) (int, error) {
	const query = `
		insert into public.map_reports (
			map_id, player_id, time, categories, comment
		) values (
			$1, $2, $3, $4, $5
		) returning id;
	`

	var id int
	err := c.pool.QueryRow(ctx, query, report.MapId, report.PlayerId, report.Timestamp, report.Categories, report.Comment).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("failed to insert report: %w", err)
	}

	return id, nil
}

func (c *PostgresClient) GetMapsBeatenLeaderboard(ctx context.Context) ([]*model.LeaderboardEntry, error) {
	const query = `
		SELECT
			s1.player_id,
			COUNT(DISTINCT s1.map_id) AS unique_maps_beaten
		FROM save_states AS s1
		JOIN maps ON s1.map_id = maps.id
		WHERE s1.deleted is null and 
		      s1.completed = TRUE and 
		      (s1.type = 'playing' or s1.type = 'verifying') and 
		      maps.published_at is not null
		GROUP BY s1.player_id
		ORDER BY unique_maps_beaten DESC
		limit 10;
	`
	return c.readLeaderboard(ctx, query)
}

func (c *PostgresClient) GetMapsBeatenLeaderboardForPlayer(ctx context.Context, playerId string) (int, error) {
	const query = `
select count(distinct map_id) as unique_maps_beaten from save_states
JOIN maps ON save_states.map_id = maps.id
where deleted is null and completed = true
and (type = 'playing' or type = 'verifying') and player_id = $1
and maps.published_at is not null;
`
	var score int
	err := c.pool.QueryRow(ctx, query, playerId).Scan(&score)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, nil
		}
		return 0, fmt.Errorf("failed to query leaderboard: %w", err)
	}

	return score, nil
}

func (c *PostgresClient) GetTopTimesLeaderboard(ctx context.Context) ([]*model.LeaderboardEntry, error) {
	const query = `
		SELECT
		    s1.player_id,
		    COUNT(distinct s1.map_id) AS top_times
		FROM(
		        SELECT map_id, (round(MIN(playtime) / 50.0) * 50)::bigint AS min_playtime FROM save_states
		        JOIN maps ON save_states.map_id = maps.id
		        WHERE deleted is null and
		            completed = TRUE and
		            playtime != 0 and
		            (type = 'playing' or type = 'verifying') and
		            maps.published_at is not null and
		            maps.deleted_at is null
		        GROUP BY map_id
		    ) AS shortest_playtimes
		        JOIN save_states AS s1
		             ON s1.map_id = shortest_playtimes.map_id
		                 AND (round(s1.playtime / 50.0) * 50)::bigint = shortest_playtimes.min_playtime
		WHERE s1.deleted is null and s1.completed = TRUE
		GROUP BY s1.player_id
		ORDER BY top_times DESC
		limit 10;
	`
	return c.readLeaderboard(ctx, query)
}

func (c *PostgresClient) GetTopTimesLeaderboardForPlayer(ctx context.Context, playerId string) (int, error) {
	const query = `
		SELECT
			COUNT(distinct s1.map_id) AS top_times
		FROM(
			SELECT map_id, (round(MIN(playtime) / 50.0) * 50)::bigint AS min_playtime FROM save_states
			JOIN maps ON save_states.map_id = maps.id
			WHERE deleted is null and
				completed = TRUE and
				playtime != 0 and
				(type = 'playing' or type = 'verifying') and
				maps.published_at is not null and
				maps.deleted_at is null
			GROUP BY map_id
		) AS shortest_playtimes
			 JOIN save_states AS s1
			 ON s1.map_id = shortest_playtimes.map_id
			 AND (round(s1.playtime / 50.0) * 50)::bigint = shortest_playtimes.min_playtime
			 and s1.player_id = $1
		WHERE s1.deleted is null and s1.completed = TRUE;
`

	var topTimes int
	err := c.pool.QueryRow(ctx, query, playerId).Scan(&topTimes)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, nil
		}
		return 0, fmt.Errorf("failed to query leaderboard: %w", err)
	}

	return topTimes, nil
}

func (c *PostgresClient) readLeaderboard(ctx context.Context, query string) ([]*model.LeaderboardEntry, error) {
	r, err := c.pool.Query(ctx, query)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return []*model.LeaderboardEntry{}, nil
		}
		return nil, fmt.Errorf("failed to query maps: %w", err)
	}
	defer r.Close()

	var result []*model.LeaderboardEntry
	for r.Next() {
		var entry model.LeaderboardEntry
		err := r.Scan(&entry.PlayerId, &entry.Score)
		if err != nil {
			return nil, fmt.Errorf("failed to read row: %w", err)
		}

		entry.Rank = len(result) + 1
		result = append(result, &entry)
	}

	return result, nil
}

func (c *PostgresClient) GetSaveStateById(ctx context.Context, mapId, playerId, saveStateId string) (*model.SaveState, error) {
	const query = `
		select
			id, map_id, player_id, type, created, updated, completed, playtime, ticks, state_v2, data_version, protocol_version
		from public.save_states 
		where deleted is null and id = $1 and map_id = $2 and player_id = $3;
	`

	return c.readSaveState(c.pool.QueryRow(ctx, query, saveStateId, mapId, playerId))
}

func (c *PostgresClient) GetLatestSaveState(ctx context.Context, mapId, playerId string, ssType model.SaveStateType) (*model.SaveState, error) {
	const query = `
		select
			id, map_id, player_id, type, created, updated, completed, playtime, ticks, state_v2, data_version, protocol_version
		from public.save_states 
		where deleted is null and map_id = $1 and player_id = $2 and type = $3
		order by updated desc limit 1;
	`

	ss, err := c.readSaveState(c.pool.QueryRow(ctx, query, mapId, playerId, ssType))
	if err != nil {
		return nil, err
	}

	if ss.Completed {
		return nil, ErrNotFound
	}

	return ss, nil
}

func (c *PostgresClient) GetBestSaveState(ctx context.Context, mapId, playerId string) (*model.SaveState, error) {
	const query = `
		select
			id, map_id, player_id, type, created, updated, completed, playtime, ticks, state_v2, data_version, protocol_version
		from public.save_states 
		where deleted is null and map_id = $1 and player_id = $2 and type = 'playing' and completed = true
		order by playtime limit 1;
	`

	return c.readSaveState(c.pool.QueryRow(ctx, query, mapId, playerId))
}

func (c *PostgresClient) GetBestSaveStateSinceBeta(ctx context.Context, mapId, playerId string) (*model.SaveState, error) {
	const query = `
		select
			id, map_id, player_id, type, created, updated, completed, playtime, ticks, state_v2, data_version, protocol_version
		from public.save_states 
		where deleted is null and map_id = $1 and player_id = $2 and type = 'playing' and completed = true
        and created > $3
		order by playtime limit 1;
	`

	betaStart, err := time.Parse(time.RFC3339, "2024-04-05T09:00:00-04:00")
	if err != nil {
		panic(err)
	}
	return c.readSaveState(c.pool.QueryRow(ctx, query, mapId, playerId, betaStart))
}

func (c *PostgresClient) GetAllSaveStates(ctx context.Context, mapId string) ([]*model.SaveState, error) {
	const query = `
		select
			id, map_id, player_id, type, created, updated, completed, playtime, ticks, state_v2, data_version, protocol_version
		from public.save_states 
		where deleted is null and completed = true and map_id = $1 and (type = 'playing' or type = 'verifying');
	`

	r, err := c.pool.Query(ctx, query, mapId)
	if err != nil {
		return nil, fmt.Errorf("failed to query maps: %w", err)
	}
	defer r.Close()

	var result []*model.SaveState
	for r.Next() {
		ss, err := c.readSaveState(r)
		if err != nil {
			return nil, fmt.Errorf("failed to read row: %w", err)
		}

		result = append(result, ss)
	}

	return result, nil
}

func (c *PostgresClient) UpdateSaveState(ctx context.Context, ss *model.SaveState) error {
	const query = `
		insert into public.save_states (id, map_id, player_id, type, created, updated, completed, playtime, ticks, state_v2, data_version, protocol_version)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		on conflict (id, map_id, player_id) do update
		set updated = excluded.updated,
		    completed = excluded.completed,
		    playtime = excluded.playtime,
		    ticks = excluded.ticks,
		    state_v2 = excluded.state_v2,
		    data_version = excluded.data_version,
		    protocol_version = excluded.protocol_version;
	`

	state, err := c.encodeSaveStateState(ss)
	if err != nil {
		return fmt.Errorf("failed to encode save state state: %w", err)
	}

	// Async update the map stats, if it fails it doesnt really matter much
	go c.UpdateMapStats(context.TODO(), ss.MapId) // todo figure out this context since it's done in the background, the parent context will be cancelled.

	return c.safeExec(ctx, query,
		ss.Id, ss.MapId, ss.PlayerId, ss.Type, ss.Created,
		ss.LastModified, ss.Completed, ss.PlayTime, ss.Ticks, state,
		ss.DataVersion, ss.ProtocolVersion,
	)
}

func (c *PostgresClient) DeleteSaveState(ctx context.Context, mapId, playerId, saveStateId string) error {
	const query = `delete from public.save_states where id = $1 and map_id = $2 and player_id = $3;`
	if err := c.safeExec(ctx, query, saveStateId, mapId, playerId); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}

		return fmt.Errorf("failed to delete state: %w", err)
	}

	return nil
}

func (c *PostgresClient) DeleteVerifyingStates(ctx context.Context, mapId string) error {
	const query = `delete from public.save_states where map_id = $1 and type = $2;`
	return c.safeExec(ctx, query, mapId, model.SaveStateTypeVerifying)
}

func (c *PostgresClient) SoftDeleteMapPlayerSaveStates(ctx context.Context, mapId, playerId string) error {
	const query = `update public.save_states set deleted = now() where deleted is null and map_id = $1 and player_id = $2;`
	return c.safeExec(ctx, query, mapId, playerId)
}

func (c *PostgresClient) SoftDeleteMapSaveStates(ctx context.Context, mapId string, onlyIncomplete bool) error {
	query := c.templateQuery(`
		update public.save_states set deleted = now() 
		where deleted is null and map_id = $1{{ if .OnlyIncomplete }} and completed = false{{ end }};
	`, struct {
		OnlyIncomplete bool
	}{onlyIncomplete})

	return c.safeExec(ctx, query, mapId)
}

func (c *PostgresClient) SoftDeletePlayerSaveStates(ctx context.Context, playerId string) error {
	const query = `update public.save_states set deleted = now() where deleted is null and player_id = $1;`
	return c.safeExec(ctx, query, playerId)
}

func (c *PostgresClient) GetCompletedMaps(ctx context.Context, playerId string) ([]string, error) {
	const query = `
		select distinct map_id
		from save_states
		where deleted is null and completed = true and player_id = $1;
	`

	r, err := c.pool.Query(ctx, query, playerId)
	if err != nil {
		return nil, fmt.Errorf("failed to query maps: %w", err)
	}
	defer r.Close()

	var result []string
	for r.Next() {
		var mapId string
		err := r.Scan(&mapId)
		if err != nil {
			return nil, fmt.Errorf("failed to read row: %w", err)
		}

		result = append(result, mapId)
	}

	return result, nil
}

func (c *PostgresClient) GetOrgById(ctx context.Context, id string) (*model.Organization, error) {
	const query = `select id, webhook_url from map_orgs where id = $1;`

	var org model.Organization
	r := c.pool.QueryRow(ctx, query, id)
	err := r.Scan(&org.Id, &org.WebhookUrl)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to fetch organization: %w", err)
	}

	return &org, nil
}

func (c *PostgresClient) readPlayerData(r pgx.Row) (*model.PlayerData, error) {
	var pd model.PlayerData
	err := r.Scan(&pd.Id, &pd.Maps, &pd.LastPlayedMap, &pd.LastEditedMap)
	return &pd, err
}

func (c *PostgresClient) encodeSaveStateState(ss *model.SaveState) (state []byte, err error) {
	if ss.Type == model.SaveStateTypeEditing {
		state, err = json.Marshal(ss.EditingState)
	} else if ss.Type == model.SaveStateTypePlaying || ss.Type == model.SaveStateTypeVerifying {
		state, err = json.Marshal(ss.PlayingState)
	} else {
		return nil, fmt.Errorf("invalid save state type: %s", ss.Type)
	}
	return
}

func (c *PostgresClient) readSaveState(r pgx.Row) (*model.SaveState, error) {
	var ss model.SaveState
	var stateBlob []byte

	// Read the fields and state blob
	err := r.Scan(
		&ss.Id, &ss.MapId, &ss.PlayerId, &ss.Type, &ss.Created,
		&ss.LastModified, &ss.Completed, &ss.PlayTime, &ss.Ticks, &stateBlob,
		&ss.DataVersion, &ss.ProtocolVersion,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to fetch save state: %w", err)
	}

	// Unmarshal the state blob based on the type
	if ss.Type == model.SaveStateTypeEditing {
		err = json.Unmarshal(stateBlob, &ss.EditingState)
	} else if ss.Type == model.SaveStateTypePlaying || ss.Type == model.SaveStateTypeVerifying {
		err = json.Unmarshal(stateBlob, &ss.PlayingState)
	} else {
		err = fmt.Errorf("invalid save state type: %s", ss.Type)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal save state state: %w", err)
	}

	return &ss, nil
}

// UpdateMapStats is responsible for updating the stats of a map.
//
// It is intended to be called asynchronously in a goroutine rather than blocking a request path.
func (c *PostgresClient) UpdateMapStats(ctx context.Context, mapId string) {
	const query = `
		insert into map_stats
		select $1	                 								  AS map_id,
		       count(distinct player_id)                              AS play_count,
		       count(distinct case when completed then player_id end) AS win_count
		from save_states
		where map_id=$1
		on conflict (map_id) do update 
		set play_count=excluded.play_count, 
		    win_count=excluded.win_count;
	`

	_, err := c.pool.Exec(ctx, query, mapId)
	if err != nil {
		c.log.Errorw("Failed to update map stats", "map_id", mapId, "error", err)
	}
}

func (c *PostgresClient) safeExec(ctx context.Context, query string, args ...interface{}) error {
	_, err := c.safeExecWithResult(ctx, query, args...)
	return err
}

func (c *PostgresClient) safeExecWithResult(ctx context.Context, query string, args ...interface{}) (pgconn.CommandTag, error) {
	// Execute in transaction if it is running in the current context
	if tx, ok := ctx.Value(txContextKey).(pgx.Tx); ok {
		return tx.Exec(ctx, query, args...)
	}

	// Otherwise execute as normal
	return c.pool.Exec(ctx, query, args...)
}

// do evaluates a query, optionally scanning into the given rows.
// If scan rows are present, the pgx.Row is never returned.
//
// ErrNotFound is always returned if there are no rows returned.
func (c *PostgresClient) do(ctx context.Context, query sq.Sqlizer, scanRows ...any) (pgx.Row, error) {
	q, args, err := query.ToSql()
	if err != nil {
		return nil, err
	}

	row := c.pool.QueryRow(ctx, q, args...)
	if err = row.Scan(scanRows...); errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return row, err
}

// do evaluates a query, optionally scanning into the given rows.
// If scan rows are present, the pgx.Row is never returned.
//
// ErrNotFound is always returned if there are no rows returned.
func (c *PostgresClient) doMulti(ctx context.Context, query sq.Sqlizer) (pgx.Rows, error) {
	return doMulti(c.pool, ctx, query)
}

func doMulti(db *pgxpool.Pool, ctx context.Context, query sq.Sqlizer) (pgx.Rows, error) {
	q, args, err := query.ToSql()
	if err != nil {
		return nil, err
	}

	return db.Query(ctx, q, args...)
}

func doMultiScan[T any](db *pgxpool.Pool, ctx context.Context, query sq.Sqlizer, scan func(t *T) []any) ([]*T, error) {
	rows, err := doMulti(db, ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	results := make([]*T, 0)
	for rows.Next() {
		var t T
		if err = rows.Scan(scan(&t)...); err != nil {
			return nil, err
		}
		results = append(results, &t)
	}

	return results, nil
}

func mustCompile(query sq.Sqlizer) string {
	str, args, err := query.ToSql()
	if err != nil {
		panic(err)
	}
	if len(args) > 0 {
		panic("unhandled sql: " + str)
	}
	return str
}
