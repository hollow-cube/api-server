package testutil

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"log"
	"strings"
	"testing"

	v1 "github.com/authzed/authzed-go/proto/authzed/api/v1"
	"github.com/authzed/authzed-go/v1"
	"github.com/authzed/grpcutil"
	"github.com/ory/dockertest/v3"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

//go:embed spicedb_schema.zed
var schema string

func RunSpiceDB(pool *dockertest.Pool) (*SpiceDB, func()) {
	resource, err := pool.RunWithOptions(&dockertest.RunOptions{
		Repository: "authzed/spicedb",
		Tag:        "v1.27.0",
		Cmd:        []string{"serve", "--datastore-engine", "memory", "--grpc-preshared-key", "secret"},
	})
	if err != nil {
		log.Fatalf("Could not start resource: %s", err)
	}

	var client *authzed.Client

	// exponential backoff-retry, because the application in the container might not be ready to accept connections yet
	if err := pool.Retry(func() error {
		var err error

		client, err = authzed.NewClient(fmt.Sprintf("localhost:%s", resource.GetPort("50051/tcp")),
			grpc.WithTransportCredentials(insecure.NewCredentials()),
			grpcutil.WithInsecureBearerToken("secret"),
		)
		if err != nil {
			return err
		}
		_, err = client.WriteSchema(context.Background(), &v1.WriteSchemaRequest{Schema: schema})
		return err
	}); err != nil {
		log.Fatalf("Could not connect to database: %s", err)
	}

	return &SpiceDB{Client: client}, func() {
		if err := pool.Purge(resource); err != nil {
			log.Printf("Could not purge resource: %s", err)
		}
	}
}

type SpiceDB struct {
	Client *authzed.Client
}

func (sdb *SpiceDB) Cleanup() {
	allRelationshipTypes := []string{"mapmaker/platform", "mapmaker/player", "mapmaker/organization", "mapmaker/map"}

	for _, relationshipType := range allRelationshipTypes {
		_, err := sdb.Client.DeleteRelationships(context.Background(), &v1.DeleteRelationshipsRequest{
			RelationshipFilter: &v1.RelationshipFilter{ResourceType: relationshipType},
		})
		if err != nil {
			log.Fatalf("Failed to wipe relationships: %s", err)
		}
	}
}

// writeRelationship allows writing test data in the form
// resource:someresource#viewer@user:somegal
func (sdb *SpiceDB) WriteRelationship(t *testing.T, relationship string, caveat *v1.ContextualizedCaveat) {
	t.Helper()

	p1 := strings.Split(relationship, "#")
	res := strings.Split(p1[0], ":")
	p2 := strings.Split(p1[1], "@")
	sub := strings.Split(p2[1], ":")

	_, err := sdb.Client.WriteRelationships(context.Background(), &v1.WriteRelationshipsRequest{Updates: []*v1.RelationshipUpdate{{
		Operation: v1.RelationshipUpdate_OPERATION_CREATE,
		Relationship: &v1.Relationship{
			Resource:       &v1.ObjectReference{ObjectType: res[0], ObjectId: res[1]},
			Relation:       p2[0],
			Subject:        &v1.SubjectReference{Object: &v1.ObjectReference{ObjectType: sub[0], ObjectId: sub[1]}},
			OptionalCaveat: caveat,
		},
	}}})
	require.NoError(t, err)
}

func (sdb *SpiceDB) ReadRelationship(t *testing.T, relationship string) *v1.Relationship {
	t.Helper()

	p1 := strings.Split(relationship, "#")
	res := strings.Split(p1[0], ":")
	p2 := strings.Split(p1[1], "@")
	sub := strings.Split(p2[1], ":")

	resp, err := sdb.Client.ReadRelationships(context.Background(), &v1.ReadRelationshipsRequest{
		RelationshipFilter: &v1.RelationshipFilter{
			ResourceType:       res[0],
			OptionalResourceId: res[1],
			OptionalRelation:   p2[0],
			OptionalSubjectFilter: &v1.SubjectFilter{
				SubjectType:       sub[0],
				OptionalSubjectId: sub[1],
			},
		},
		OptionalLimit: 1,
	})
	require.NoError(t, err)
	result, err := resp.Recv()
	if errors.Is(err, io.EOF) {
		return nil
	}
	require.NoError(t, err)
	return result.Relationship
}
