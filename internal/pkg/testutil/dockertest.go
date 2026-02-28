package testutil

import (
	"errors"
	"log"
	"os"
	"testing"

	"github.com/ory/dockertest/v3"
)

var ErrMock = errors.New("mock error")

var code = 0

func Init(m *testing.M) (*dockertest.Pool, func(), func()) {
	pool, err := dockertest.NewPool("")
	if err != nil {
		log.Fatalf("Could not construct pool: %s", err)
	}

	// uses pool to try to connect to Docker
	err = pool.Client.Ping()
	if err != nil {
		log.Fatalf("Could not connect to Docker: %s", err)
	}

	return pool,
		func() {
			code = m.Run()
		},
		func() {
			os.Exit(code)
		}
}
