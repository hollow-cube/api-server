package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/hollow-cube/hc-services/services/map/pkg/schematic"
	"go.uber.org/zap"
)

type httpClient struct {
	address string
}

func (h *httpClient) GetLegacyMaps(ctx context.Context, playerId string) ([]*LegacyMap, []byte, error) {
	endpoint := fmt.Sprintf("%s/v1/internal/legacy/%s", h.address, playerId)
	resp, err := doRequest(ctx, "GET", endpoint, nil, nil)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		return readTypedListAndRaw[LegacyMap](resp.Body)
	case http.StatusNotFound:
		return []*LegacyMap{}, []byte("[]"), nil
	default:
		body, _ := io.ReadAll(resp.Body)
		return nil, nil, fmt.Errorf("unknown error (%d): %s", resp.StatusCode, body)
	}
}

func (h *httpClient) GetLegacyMapWorld(ctx context.Context, playerId, legacyMapId string) ([]byte, error) {
	endpoint := fmt.Sprintf("%s/v1/internal/legacy/%s/%s/world", h.address, playerId, legacyMapId)
	resp, err := doRequest(ctx, "GET", endpoint, nil, nil)
	if err != nil {
		return nil, nil
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		return io.ReadAll(resp.Body)
	case http.StatusNotFound:
		return nil, ErrMapNotFound
	default:
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("unknown error (%d): %s", resp.StatusCode, body)
	}
}

func (h *httpClient) TFListSchematics(ctx context.Context, playerId string) ([]*SchematicHeader, []byte, error) {
	endpoint := fmt.Sprintf("%s/v1/internal/terraform/schem/%s", h.address, playerId)
	resp, err := doRequest(ctx, "GET", endpoint, nil, nil)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		return readTypedListAndRaw[SchematicHeader](resp.Body)
	case http.StatusNotFound:
		return nil, nil, ErrSchematicNotFound
	default:
		body, _ := io.ReadAll(resp.Body)
		return nil, nil, fmt.Errorf("unknown error (%d): %s", resp.StatusCode, body)
	}
}

func (h *httpClient) TFDownloadSchematic(ctx context.Context, playerId, name string) ([]byte, error) {
	endpoint := fmt.Sprintf("%s/v1/internal/terraform/schem/%s/%s", h.address, playerId, name)
	resp, err := doRequest(ctx, "GET", endpoint, nil, nil)
	if err != nil {
		return nil, nil
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		return io.ReadAll(resp.Body)
	case http.StatusNotFound:
		return nil, ErrSchematicNotFound
	default:
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("unknown error (%d): %s", resp.StatusCode, body)
	}
}

func (h *httpClient) TFUploadSchematic(ctx context.Context, playerId, name string, schemData []byte) error {
	// TODO: The endpoint should handle schematics, this is just a workaround for now.
	parsedSchem, err := schematic.Read(bytes.NewReader(schemData), true)
	if err != nil {
		zap.S().Errorw("failed to parse schematic", "error", err)
		return ErrInvalidSchematic
	}

	endpoint := fmt.Sprintf("%s/v1/internal/terraform/schem/%s/%s?dimx=%d&dimy=%d&dimz=%d&size=%d",
		h.address, playerId, name, parsedSchem.Width, parsedSchem.Height, parsedSchem.Length, len(schemData))
	resp, err := doRequest(ctx, "POST", endpoint, bytes.NewReader(schemData), map[string]string{
		"content-type": "application/vnd.terraform.schematic",
	})
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		return nil
	case http.StatusConflict:
		return ErrSchematicAlreadyExists
	case http.StatusBadRequest:
		return ErrInvalidSchematic
	default:
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("unknown error (%d): %s", resp.StatusCode, body)
	}
}

func doRequest(ctx context.Context, method, endpoint string, body interface{}, headers map[string]string) (*http.Response, error) {
	var bodyRaw io.Reader
	if body != nil {
		if b, ok := body.(io.Reader); ok {
			bodyRaw = b
		} else {
			buf := new(bytes.Buffer)
			if err := json.NewEncoder(buf).Encode(body); err != nil {
				return nil, fmt.Errorf("failed to encode request: %w", err)
			}
			bodyRaw = buf
		}
	}

	req, err := http.NewRequestWithContext(ctx, method, endpoint, bodyRaw)
	if err != nil {
		return nil, fmt.Errorf("malformed request: %w", err)
	}

	if len(headers) > 0 {
		for k, v := range headers {
			req.Header.Add(k, v)
		}
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}

	return resp, nil
}

func readTypedAndRaw[T any](body io.Reader) (*T, []byte, error) {
	raw, err := io.ReadAll(body)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read response: %w", err)
	}

	var typed T
	if err = json.Unmarshal(raw, &typed); err != nil {
		return nil, nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return &typed, raw, nil
}

func readTypedListAndRaw[T any](body io.Reader) ([]*T, []byte, error) {
	raw, err := io.ReadAll(body)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read response: %w", err)
	}

	var typed []*T
	if err = json.Unmarshal(raw, &typed); err != nil {
		return nil, nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return typed, raw, nil
}
