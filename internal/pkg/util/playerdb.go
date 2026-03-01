package util

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

func GetPlayerInfo(ctx context.Context, playerId string) (username, uuid, avatar string, err error) {
	endpoint := fmt.Sprintf("https://playerdb.co/api/player/minecraft/%s", playerId)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", "", "", err
	}

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", "", "", err
	}
	defer res.Body.Close()

	var playerInfo struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
		Data    struct {
			Player struct {
				Id       string `json:"id"`
				Username string `json:"username"`
				Avatar   string `json:"avatar"`
			} `json:"player"`
		} `json:"data"`
	}
	if err = json.NewDecoder(res.Body).Decode(&playerInfo); err != nil {
		return "", "", "", err
	}
	if !playerInfo.Success {
		return "", "", "", fmt.Errorf("playerdb.co error: %s", playerInfo.Message)
	}

	return playerInfo.Data.Player.Username, playerInfo.Data.Player.Id, playerInfo.Data.Player.Avatar, nil
}
