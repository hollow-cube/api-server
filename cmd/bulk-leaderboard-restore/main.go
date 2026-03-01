package main

import (
	"context"
	"fmt"

	"github.com/hollow-cube/api-server/internal/mapdb"
	"github.com/hollow-cube/api-server/internal/pkg/common"
	"github.com/hollow-cube/api-server/internal/pkg/model"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/rueidis"
)

func main() {
	config, err := pgxpool.ParseConfig("postgresql://postgres:32hJ6h76kFcyunsDBcAq9rB5CzRRwAs4CTgGq8at9sxazf3UwQFCFPT8puBJLjQG@localhost:5433/map-service")
	if err != nil {
		panic(fmt.Errorf("failed to parse postgres config: %w", err))
	}

	pool, err := pgxpool.NewWithConfig(context.Background(), config)
	if err != nil {
		panic(fmt.Errorf("failed to connect to postgres: %w", err))
	}

	mapStore := mapdb.New(pool)

	c, err := rueidis.NewClient(rueidis.ClientOption{
		InitAddress: []string{"localhost:6380"},
	})
	if err != nil {
		panic(err)
	}

	maps, _ := mapStore.GetAllMaps(context.Background())
	for i, m := range maps {
		println(m.ID, i+1, "/", len(maps))

		leaderboardKey := mapLeaderboardKey(m.ID, "playtime")

		// Confirm that the map is a parkour map, otherwise do nothing (its not currently an error)
		if m.OptVariant != string(model.Parkour) {
			continue
		}

		// Fetch all save states for this map and rewrite them into redis
		// TODO: This should really be paged it could be a ton of entries.
		saveStates, err := mapStore.GetAllSaveStates(context.Background(), m.ID)
		if err != nil {
			panic(fmt.Errorf("failed to fetch save states: %w", err))
		}
		if len(saveStates) == 0 {
			continue
		}

		cmds := make(rueidis.Commands, len(saveStates)+1)
		cmds[0] = c.B().Del().Key(leaderboardKey).Build()
		for i, saveState := range saveStates {
			cmds[i+1] = c.B().Zadd().Key(leaderboardKey).Lt().ScoreMember().
				ScoreMember(float64(saveState.Playtime), string(common.UUIDToBin(saveState.PlayerID))).Build()
		}
		for _, resp := range c.DoMulti(context.Background(), cmds...) {
			if err = resp.Error(); err != nil {
				panic(fmt.Errorf("failed to write save states to redis: %w", err))
			}
		}
	}
}

func mapLeaderboardKey(mapId, lbType string) string {
	return fmt.Sprintf("map:%s:lb_%s", mapId, lbType)
}
