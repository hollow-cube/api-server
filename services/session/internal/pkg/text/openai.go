package text

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"go.uber.org/zap"
)

const openaiModerationEndpoint = "https://api.openai.com/v1/moderations"

type OpenAIFilter struct {
	apiKey string
}

func NewOpenAIFilter(apiKey string) *OpenAIFilter {
	return &OpenAIFilter{apiKey}
}

type openaiModerationResponse struct {
	Id      string `json:"id"`
	Model   string `json:"model"`
	Results []struct {
		Flagged bool `json:"flagged"`
	} `json:"results"`
}

func (o *OpenAIFilter) Test(ctx context.Context, text string) (result Result) {
	result.Engine = "openai"

	body := strings.NewReader(fmt.Sprintf(`{"input": "%s"}`, text))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, openaiModerationEndpoint, body)
	if err != nil {
		zap.S().Errorw("failed to create request", "error", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer: %s", o.apiKey))

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		zap.S().Errorw("failed to send request", "error", err)
		return
	}
	defer res.Body.Close()

	var openaiRes openaiModerationResponse
	if err := json.NewDecoder(res.Body).Decode(&openaiRes); err != nil {
		zap.S().Errorw("failed to decode response", "error", err)
		return
	}

	if len(openaiRes.Results) == 0 {
		zap.S().Errorw("no results in response")
		return
	}

	result.Matched = openaiRes.Results[0].Flagged

	return
}
