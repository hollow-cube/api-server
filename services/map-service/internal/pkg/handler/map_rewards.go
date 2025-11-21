package handler

import (
	"context"
	"math/rand"

	"github.com/hollow-cube/hc-services/services/map-service/internal/pkg/model"
	playerServiceV2 "github.com/hollow-cube/hc-services/services/player-service/api/v2/intnl"
)

var (
	coinsByDifficulty = map[model.MapDifficulty]int{
		model.MapDifficultyNightmare: 1100,
		model.MapDifficultyExpert:    700,
		model.MapDifficultyHard:      400,
		model.MapDifficultyMedium:    200,
		model.MapDifficultyEasy:      100,
	}
	expByDifficulty = map[model.MapDifficulty]int{
		model.MapDifficultyNightmare: 325,
		model.MapDifficultyExpert:    200,
		model.MapDifficultyHard:      125,
		model.MapDifficultyMedium:    75,
		model.MapDifficultyEasy:      50,
	}
	normalMaterials = []string{"cloth", "gem", "goo", "metal", "string"}
	boneFragment    = "bone_fragment"
	flowerPetal     = "flower_petal"
	sugarCube       = "sugar_cube"
	infernalFlame   = "infernal_flame"
	nightmareFuel   = "nightmare_fuel"
)

func (h *InternalHandler) computeMapRewards(ctx context.Context, m *model.Map, isFirstCompletion bool, ss *model.SaveState) (*playerServiceV2.PlayerInventory, map[string]interface{}, error) {
	//todo reenable actual reward computation in the future
	if ss.PlayTime < 10_000 || true {
		return &playerServiceV2.PlayerInventory{}, nil, nil
	}

	difficulty := m.Difficulty()
	coins := coinsByDifficulty[difficulty]
	exp := expByDifficulty[difficulty]

	itemChance := 0.25
	if isFirstCompletion {
		// Quality maps get a 3x bonus for the first completion
		if m.QualityOverride > 0 {
			coins *= 3
			exp *= 3
		}
	} else {
		// Not first completion is reduced by 90%
		coins = int(float64(coins) * 0.1)
		exp = int(float64(exp) * 0.1)
		itemChance = 0.05
	}

	// Add backpack items
	backpack := map[string]interface{}{}
	if rand.Float64() > (1.0 - itemChance) {
		items := normalMaterials

		if m.Settings.SubVariant != nil && *m.Settings.SubVariant == model.ParkourDropper {
			items = append(items, boneFragment)
		}
		if difficulty == model.MapDifficultyEasy {
			items = append(items, flowerPetal)
		}
		if m.Settings.OnlySprint {
			items = append(items, sugarCube)
		}
		if difficulty == model.MapDifficultyExpert {
			items = append(items, infernalFlame)
		}
		if difficulty == model.MapDifficultyNightmare {
			items = append(items, nightmareFuel)
		}

		backpack[items[rand.Intn(len(items))]] = 1
	}

	return &playerServiceV2.PlayerInventory{
			Coins:    &coins,
			Exp:      &exp,
			Backpack: &backpack,
		}, map[string]interface{}{
			"mapId":             m.Id,
			"difficulty":        difficulty,
			"isFirstCompletion": isFirstCompletion,
		}, nil
}
