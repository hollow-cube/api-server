package authz

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	v1 "github.com/authzed/authzed-go/proto/authzed/api/v1"
	"github.com/authzed/authzed-go/v1"
	authzedUtil "github.com/hollow-cube/hc-services/libraries/common/pkg/authzed"
	"google.golang.org/protobuf/types/known/structpb"
)

var _ Client = (*SpiceDBClient)(nil)

type SpiceDBClient struct {
	sdb *authzed.Client
}

func NewSpiceDBClient(address string, token string, secure bool) (*SpiceDBClient, error) {
	if secure {
		panic("secure spicedb connection not implemented")
	}

	sdb, err := authzedUtil.NewAuthzedClient(address, token, secure)
	if err != nil {
		return nil, fmt.Errorf("failed to create SpiceDB client: %w", err)
	}

	return &SpiceDBClient{sdb}, nil
}

// todo in the future this can be replaced witht he CheckBulkPermission call to SpiceDB
// we are not on a version that supports this... and we can't use the deprecated BulkCheckPermission since we'd have to gen
// the gRPC client ourselves not use the official one.
func (c *SpiceDBClient) MultiCheckPlatformPermission(ctx context.Context, userIds []string, cacheKey string, perm PlatformPermission) (map[string]State, error) {
	if len(userIds) == 0 {
		return make(map[string]State), nil
	}

	wg := sync.WaitGroup{}
	mu := sync.Mutex{}
	results := make(map[string]State, len(userIds))
	errs := make([]error, 0, len(userIds))

	// limit to 15 concurrent reqs
	sem := make(chan struct{}, 15)

	for _, userId := range userIds {
		wg.Add(1)
		go func(userId string) {
			defer wg.Done()

			sem <- struct{}{}
			defer func() { <-sem }()

			state, err := c.CheckPlatformPermission(ctx, userId, cacheKey, perm)

			// Lock to write to result map or errors slice
			mu.Lock()
			defer mu.Unlock()

			if err != nil {
				errs = append(errs, err)
			} else {
				results[userId] = state
			}
		}(userId)
	}

	wg.Wait()

	if len(errs) > 0 {
		return nil, fmt.Errorf("failed to check permissions: %w", errors.Join(errs...))
	}

	return results, nil
	// The below code should work (but untested) once we upgrade SpiceDB
	//if _, ok := PlatformPermissionValidationMap[perm]; !ok {
	//	return nil, fmt.Errorf("%w, %s", ErrNoSuchPermission, perm)
	//}
	//
	//var consistency *v1.Consistency
	//if cacheKey != "" {
	//	consistency = &v1.Consistency{Requirement: &v1.Consistency_AtLeastAsFresh{AtLeastAsFresh: &v1.ZedToken{Token: cacheKey}}}
	//}
	//
	//checkItems := make([]*v1.CheckBulkPermissionsRequestItem, len(userIds))
	//for i, userId := range userIds {
	//	checkItems[i] = &v1.CheckBulkPermissionsRequestItem{
	//		Resource:   &v1.ObjectReference{ObjectType: "mapmaker/platform", ObjectId: "0"},
	//		Permission: string(perm),
	//		Subject:    &v1.SubjectReference{Object: &v1.ObjectReference{ObjectType: "mapmaker/player", ObjectId: userId}},
	//	}
	//}
	//
	//resp, err := c.sdb.CheckBulkPermissions(ctx, &v1.CheckBulkPermissionsRequest{
	//	Consistency: consistency,
	//	Items:       checkItems,
	//})
	//if err != nil {
	//	return nil, fmt.Errorf("failed to check permission: %w", err)
	//}
	//
	//results := make(map[string]State, len(userIds))
	//for _, pair := range resp.Pairs {
	//	switch v := pair.GetResponse().(type) {
	//	case *v1.CheckBulkPermissionsPair_Item:
	//		results[pair.Request.Subject.Object.ObjectId] = stateFromSpiceDB(v.Item.Permissionship)
	//	case *v1.CheckBulkPermissionsPair_Error:
	//		return nil, fmt.Errorf("failed to check permission: %+v", v.Error)
	//	default:
	//		return nil, fmt.Errorf("unexpected response type: %T", v)
	//	}
	//}
	//return results, nil
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

	return stateFromSpiceDB(resp.Permissionship), nil
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
	state := stateFromSpiceDB(resp.Permissionship) // Conditional is valid because admin perms are always given conditionally for audit logging.
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

func stateFromSpiceDB(permissionship v1.CheckPermissionResponse_Permissionship) State {
	switch permissionship {
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
