package authz

import (
	"context"
	"errors"
	"fmt"
	"sync"

	v1 "github.com/authzed/authzed-go/proto/authzed/api/v1"
	"github.com/authzed/authzed-go/v1"
	authzedUtil "github.com/hollow-cube/hc-services/libraries/common/pkg/authzed"
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
