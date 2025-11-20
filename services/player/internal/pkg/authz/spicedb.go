package authz

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	v1 "github.com/authzed/authzed-go/proto/authzed/api/v1"
	"github.com/authzed/authzed-go/v1"
	"github.com/authzed/grpcutil"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/structpb"
)

var _ Client = (*SpiceDBClient)(nil)

type SpiceDBClient struct {
	sdb *authzed.Client
}

func NewSpiceDBClient(address string, token string, secure bool) (*SpiceDBClient, error) {
	var sdb *authzed.Client
	var err error
	if !secure {
		sdb, err = authzed.NewClient(address,
			grpc.WithTransportCredentials(insecure.NewCredentials()),
			grpcutil.WithInsecureBearerToken(token),
		)
	} else {
		panic("secure spicedb connection not implemented")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to create SpiceDB client: %w", err)
	}
	return NewSpiceDBClientFromClient(sdb), nil
}

func NewSpiceDBClientFromClient(client *authzed.Client) *SpiceDBClient {
	return &SpiceDBClient{client}
}

func (c *SpiceDBClient) CheckPlatformPermission(ctx context.Context, userId, cacheKey string, perm PlatformPermission) (State, error) {
	if _, ok := PlatformPermissionValidationMap[perm]; !ok {
		return Deny, fmt.Errorf("%w, %s", ErrNoSuchPermission, perm)
	}

	var consistency *v1.Consistency
	if cacheKey != "" {
		consistency = &v1.Consistency{Requirement: &v1.Consistency_AtLeastAsFresh{AtLeastAsFresh: &v1.ZedToken{Token: cacheKey}}}
	}

	resp, err := c.sdb.CheckPermission(ctx, &v1.CheckPermissionRequest{
		Consistency: consistency,
		Resource:    &v1.ObjectReference{ObjectType: "mapmaker/platform", ObjectId: "0"},
		Permission:  string(perm),
		Subject:     &v1.SubjectReference{Object: &v1.ObjectReference{ObjectType: "mapmaker/player", ObjectId: userId}},
	})
	if err != nil {
		return Deny, fmt.Errorf("failed to check permission: %w", err)
	}
	return stateFromSpiceDB(resp), nil
}

func (c *SpiceDBClient) HasHypercube(ctx context.Context, playerId, cacheKey string) (bool, error) {
	var consistency *v1.Consistency
	if cacheKey != "" {
		consistency = &v1.Consistency{Requirement: &v1.Consistency_AtLeastAsFresh{AtLeastAsFresh: &v1.ZedToken{Token: cacheKey}}}
	}

	caveatContext, err := structpb.NewStruct(map[string]interface{}{
		"never_set":    true, //todo audit log hack impl
		"current_time": time.Now().Format(time.RFC3339),
	})
	if err != nil {
		return false, fmt.Errorf("failed to create caveat context: %w", err)
	}

	resp, err := c.sdb.CheckPermission(ctx, &v1.CheckPermissionRequest{
		Consistency: consistency,
		Resource:    &v1.ObjectReference{ObjectType: "mapmaker/platform", ObjectId: "0"},
		Permission:  "has_hypercube",
		Subject:     &v1.SubjectReference{Object: &v1.ObjectReference{ObjectType: "mapmaker/player", ObjectId: playerId}},
		Context:     caveatContext,
	})
	if err != nil {
		return false, fmt.Errorf("failed to check permission: %w", err)
	}
	state := stateFromSpiceDB(resp) // Conditional is valid because admin perms are always given conditionally for audit logging.
	return state == Allow || state == Conditional, nil
}

func (c *SpiceDBClient) GetHypercubeStats(ctx context.Context, playerId string, cacheKey string) (time.Time, time.Duration, error) {
	var consistency *v1.Consistency
	if cacheKey != "" {
		consistency = &v1.Consistency{Requirement: &v1.Consistency_AtLeastAsFresh{AtLeastAsFresh: &v1.ZedToken{Token: cacheKey}}}
	} else {
		consistency = &v1.Consistency{Requirement: &v1.Consistency_FullyConsistent{FullyConsistent: true}}
	}

	baseRes, err := c.sdb.ReadRelationships(ctx, &v1.ReadRelationshipsRequest{
		Consistency:   consistency,
		OptionalLimit: 1,
		RelationshipFilter: &v1.RelationshipFilter{
			ResourceType:       "mapmaker/platform",
			OptionalResourceId: "0",
			OptionalRelation:   "hypercube",
			OptionalSubjectFilter: &v1.SubjectFilter{
				SubjectType:       "mapmaker/player",
				OptionalSubjectId: playerId,
			},
		},
	})
	if err != nil {
		return time.Time{}, 0, fmt.Errorf("failed to read relationships: %w", err)
	}

	res, err := baseRes.Recv()
	if err != nil {
		if errors.Is(err, io.EOF) {
			return time.Time{}, 0, ErrNotFound
		}
		return time.Time{}, 0, fmt.Errorf("failed to receive relationships: %w", err)
	}

	caveatContext := res.Relationship.OptionalCaveat.Context.Fields
	startTimeStr := caveatContext["start_time"].GetStringValue()
	startTime, err := time.Parse(time.RFC3339, startTimeStr)
	if err != nil {
		return time.Time{}, 0, fmt.Errorf("failed to parse start time: %w", err)
	}

	termStr := caveatContext["term"].GetStringValue()
	term, err := time.ParseDuration(termStr)
	if err != nil {
		return time.Time{}, 0, fmt.Errorf("failed to parse term: %w", err)
	}

	return startTime, term, nil
}

func (c *SpiceDBClient) AppendHypercube(ctx context.Context, playerId string, addedTerm time.Duration, cacheKey string) error {
	startTime, oldTerm, err := c.GetHypercubeStats(ctx, playerId, cacheKey)
	if errors.Is(err, ErrNotFound) {
		startTime = time.Now()
	} else if err != nil {
		return fmt.Errorf("failed to get hypercube stats: %w", err)
	}

	// It is valid to provide a negative `addedTerm`, meaning the duration can be negative. Additionally,
	// it means that `start_time + duration < now` in which case it is expired and we should remove it.
	newTerm := oldTerm + addedTerm

	operation := v1.RelationshipUpdate_OPERATION_TOUCH // Update existing
	if newTerm <= 0 || startTime.Add(newTerm).Before(time.Now()) {
		operation = v1.RelationshipUpdate_OPERATION_DELETE // Remove expired
	}

	caveatContext, err := structpb.NewStruct(map[string]interface{}{
		"start_time": startTime.Format(time.RFC3339),
		"term":       newTerm.String(),
	})
	if err != nil {
		return fmt.Errorf("failed to create caveat context: %w", err)
	}

	_, err = c.sdb.WriteRelationships(ctx, &v1.WriteRelationshipsRequest{Updates: []*v1.RelationshipUpdate{{
		// A touch operation replaces the caveat state with the provided one, a test confirms this.
		Operation: operation,
		Relationship: &v1.Relationship{
			Resource:       &v1.ObjectReference{ObjectType: "mapmaker/platform", ObjectId: "0"},
			Relation:       "hypercube",
			Subject:        &v1.SubjectReference{Object: &v1.ObjectReference{ObjectType: "mapmaker/player", ObjectId: playerId}},
			OptionalCaveat: &v1.ContextualizedCaveat{CaveatName: "is_not_expired", Context: caveatContext},
		},
	}}})
	if err != nil {
		return fmt.Errorf("failed to write relationships: %w", err)
	}

	return nil
}

func (c *SpiceDBClient) UnlockUpgrade(ctx context.Context, playerId, upgradeId, cacheKey string) error {
	return c.writeUpgradeRelationship(ctx, playerId, upgradeId, cacheKey, v1.RelationshipUpdate_OPERATION_TOUCH)
}

func (c *SpiceDBClient) RemoveUpgrade(ctx context.Context, playerId, upgradeId, cacheKey string) error {
	return c.writeUpgradeRelationship(ctx, playerId, upgradeId, cacheKey, v1.RelationshipUpdate_OPERATION_DELETE)
}

func (c *SpiceDBClient) writeUpgradeRelationship(ctx context.Context, playerId, upgradeId, cacheKey string, operation v1.RelationshipUpdate_Operation) error {
	_, err := c.sdb.WriteRelationships(ctx, &v1.WriteRelationshipsRequest{Updates: []*v1.RelationshipUpdate{{
		// A touch operation replaces the caveat state with the provided one, a test confirms this.
		Operation: operation,
		Relationship: &v1.Relationship{
			Resource: &v1.ObjectReference{ObjectType: "mapmaker/platform", ObjectId: "0"},
			Relation: fmt.Sprintf("u_%s", upgradeId),
			Subject:  &v1.SubjectReference{Object: &v1.ObjectReference{ObjectType: "mapmaker/player", ObjectId: playerId}},
		},
	}}})
	if err != nil {
		return fmt.Errorf("failed to write relationships: %w", err)
	}
	return nil
}

func stateFromSpiceDB(resp *v1.CheckPermissionResponse) State {
	switch resp.Permissionship {
	case v1.CheckPermissionResponse_PERMISSIONSHIP_HAS_PERMISSION:
		return Allow
	case v1.CheckPermissionResponse_PERMISSIONSHIP_CONDITIONAL_PERMISSION:
		return Conditional
	case v1.CheckPermissionResponse_PERMISSIONSHIP_NO_PERMISSION:
		return Deny
	case v1.CheckPermissionResponse_PERMISSIONSHIP_UNSPECIFIED:
		return Unspecified
	default:
		return Deny
	}
}
