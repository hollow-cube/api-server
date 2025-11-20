package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/Tnze/go-mc/nbt"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	println("Hello, World!")

	var err error
	pool, err := pgxpool.New(context.Background(), "postgresql://postgres:postgres@localhost:5432/map-service")
	if err != nil {
		panic(fmt.Errorf("failed to connect to postgres: %w", err))
	}
	defer pool.Close()

	files, err := os.ReadDir("/Users/matt/dev/projects/hollowcube/oldboxgetter/out_results_with_shape/new_old")
	if err != nil {
		panic(err)
	}

	for _, file := range files {
		content, err := os.ReadFile("/Users/matt/dev/projects/hollowcube/oldboxgetter/out_results_with_shape/new_old/" + file.Name())
		if err != nil {
			panic(err)
		}
		unzipped, err := gzip.NewReader(bytes.NewReader(content))
		if err != nil {
			panic(err)
		}
		unzippedRaw, err := io.ReadAll(unzipped)
		if err != nil {
			panic(err)
		}

		var res map[string]interface{}
		err = nbt.Unmarshal(unzippedRaw, &res)
		if err != nil {
			panic(err)
		}

		schemData, ok := res["Schematic"].(map[string]interface{})
		if !ok {
			panic("no schematic data")
		}
		metadata, ok := schemData["Metadata"].(map[string]interface{})
		if !ok {
			panic("no metadata")
		}
		shapeStr, ok := metadata["BoxShape"].(string)
		var shape string
		if shapeStr == "STRAIGHT" {
			shape = "01"
		} else if shapeStr == "RIGHT" {
			shape = "001"
		}

		authorStr, ok := metadata["BoxAuthor"].(string)
		var author *string
		if authorStr != "" {
			author = &authorStr
		}

		_, err = pool.Exec(context.Background(), "INSERT INTO obungus_pending_boxes (id, created_at, shape, legacy_username, schematic_data) VALUES ($1, $2, $3, $4, $5)",
			uuid.NewString(), time.Now(), shape, author, content)
		if err != nil {
			panic(err)
		}

		println(fmt.Sprintf("%v", metadata))
	}
}
