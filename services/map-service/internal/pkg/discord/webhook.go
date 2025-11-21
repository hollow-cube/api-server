package discord

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
)

const MaxEmbedFields = 25

type Embed struct {
	Title       string       `json:"title"`
	Description string       `json:"description"`
	Timestamp   string       `json:"timestamp"`
	Color       int          `json:"color"`
	Footer      EmbedFooter  `json:"footer"`
	Fields      []EmbedField `json:"fields"`
}

type EmbedFooter struct {
	Text    string `json:"text"`
	IconUrl string `json:"icon_url"`
}

type EmbedField struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Inline bool   `json:"inline"`
}

func SendWebhookEmbed(ctx context.Context, url string, embed *Embed) error {
	body := map[string]interface{}{"embeds": []*Embed{embed}}
	encoded, err := json.Marshal(body)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url+"?wait=true", bytes.NewReader(encoded))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}

	if resp.StatusCode != http.StatusOK {
		return errors.New("non-200 status code")
	}

	return nil
}
