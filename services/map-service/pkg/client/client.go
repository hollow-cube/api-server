package client

import (
	"context"
	"errors"
)

var (
	ErrMapNotFound = errors.New("map not found")

	//todo should just reuse v1 errors once they are ported to use common errors
	ErrSchematicNotFound      = errors.New("schematic not found")
	ErrInvalidSchematic       = errors.New("invalid schematic")
	ErrSchematicAlreadyExists = errors.New("schematic already exists")
)

// Client is a (partial, should generate it at some point from the spec) client implementation of
// the map service internal http api.
//
// Note: All responses return both their typed response and the raw response body. This is because
// the typed response can become out of date, and if the response is being forwarded, we need to
// maintain the newer fields (so that we don't need to update both the player svc and clients).
type Client interface {
	// TODO

	GetLegacyMaps(ctx context.Context, playerId string) ([]*LegacyMap, []byte, error)
	GetLegacyMapWorld(ctx context.Context, playerId, legacyMapId string) ([]byte, error)

	// Terraform Endpoints

	TFListSchematics(ctx context.Context, playerId string) ([]*SchematicHeader, []byte, error)
	TFDownloadSchematic(ctx context.Context, playerId, name string) ([]byte, error)
	// TFUploadSchematic uploads a schematic to the terraform backend for use by the player.
	TFUploadSchematic(ctx context.Context, playerId, name string, schematic []byte) error
}

type LegacyMap struct {
	Id   string `json:"id"`
	Name string `json:"name"`
}

type SchematicHeader struct {
	Name       string `json:"name"`
	Size       int64  `json:"size"`
	Dimensions struct {
		X int `json:"x"`
		Y int `json:"y"`
		Z int `json:"z"`
	}
}

func NewClient(address string) Client {
	return &httpClient{address}
}
