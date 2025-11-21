package storage

import (
	"testing"

	"github.com/hollow-cube/hc-services/services/map-service/internal/pkg/model"
	"github.com/stretchr/testify/require"
)

func TestBuildSearchQueryV3(t *testing.T) {

	t.Run("minimal search", func(t *testing.T) {
		params := SearchQueryV3{PageSize: 2, Variant: []model.MapVariant{model.Parkour}}
		sql, args, err := buildSearchV3Query(params).ToSql()
		require.NoError(t, err)

		require.Equal(t, "SELECT m.id, m.owner, m.m_type, m.created_at, m.updated_at, m.published_id, m.published_at, m.quality_override, m.opt_name, m.size, m.opt_icon, m.opt_variant, m.opt_subvariant, m.opt_only_sprint, m.opt_no_sprint, m.opt_no_jump, m.opt_no_sneak, m.opt_boat, m.opt_tags, COALESCE(stats.play_count, 0) AS play_count, COALESCE(stats.clear_rate, 0) AS clear_rate, GREATEST(COALESCE(likes.total_likes, 0), 0) AS likes,  CASE WHEN play_count < 10 THEN -1 WHEN clear_rate < 0.05 THEN 4 WHEN clear_rate < 0.25 THEN 3 WHEN clear_rate < 0.5 THEN 2 WHEN clear_rate < 0.75 THEN 1 ELSE 0 END AS difficulty  FROM public.maps m LEFT JOIN (SELECT map_id, play_count, win_count, win_count::float8 / NULLIF(play_count, 0)::float8 as clear_rate FROM public.map_stats GROUP BY map_id) stats ON m.id = stats.map_id LEFT JOIN (SELECT map_id, SUM(CASE WHEN rating = 1 THEN 1 WHEN rating = 2 THEN -1 ELSE 0 END) AS total_likes FROM public.map_ratings GROUP BY map_id) likes ON m.id = likes.map_id WHERE (deleted_at IS NULL AND published_id IS NOT NULL AND opt_variant IN ($1)) ORDER BY m.quality_override asc, likes desc LIMIT 3 OFFSET 0", sql)
		require.ElementsMatch(t, []interface{}{model.Parkour}, args)
	})

	t.Run("difficulty filter search", func(t *testing.T) {
		params := SearchQueryV3{PageSize: 2, Variant: []model.MapVariant{model.Parkour}, Difficulty: []model.MapDifficulty{model.MapDifficultyEasy, model.MapDifficultyNightmare}}
		sql, args, err := buildSearchV3Query(params).ToSql()
		require.NoError(t, err)

		require.Equal(t, "SELECT m.id, m.owner, m.m_type, m.created_at, m.updated_at, m.published_id, m.published_at, m.quality_override, m.opt_name, m.size, m.opt_icon, m.opt_variant, m.opt_subvariant, m.opt_only_sprint, m.opt_no_sprint, m.opt_no_jump, m.opt_no_sneak, m.opt_boat, m.opt_tags, COALESCE(stats.play_count, 0) AS play_count, COALESCE(stats.clear_rate, 0) AS clear_rate, GREATEST(COALESCE(likes.total_likes, 0), 0) AS likes,  CASE WHEN play_count < 10 THEN -1 WHEN clear_rate < 0.05 THEN 4 WHEN clear_rate < 0.25 THEN 3 WHEN clear_rate < 0.5 THEN 2 WHEN clear_rate < 0.75 THEN 1 ELSE 0 END AS difficulty  FROM public.maps m LEFT JOIN (SELECT map_id, play_count, win_count, win_count::float8 / NULLIF(play_count, 0)::float8 as clear_rate FROM public.map_stats GROUP BY map_id) stats ON m.id = stats.map_id LEFT JOIN (SELECT map_id, SUM(CASE WHEN rating = 1 THEN 1 WHEN rating = 2 THEN -1 ELSE 0 END) AS total_likes FROM public.map_ratings GROUP BY map_id) likes ON m.id = likes.map_id WHERE (deleted_at IS NULL AND published_id IS NOT NULL AND opt_variant IN ($1)) ORDER BY m.quality_override asc, likes desc LIMIT 3 OFFSET 0", sql)
		require.ElementsMatch(t, []interface{}{model.Parkour, model.MapDifficultyEasy, model.MapDifficultyNightmare}, args)
	})
}
