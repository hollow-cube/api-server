package authz

import (
	"context"
	"fmt"
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

func (c *SpiceDBClient) SetMapOwner(ctx context.Context, mapId string, userId string) (string, error) {
	resp, err := c.sdb.WriteRelationships(ctx, &v1.WriteRelationshipsRequest{
		Updates: []*v1.RelationshipUpdate{{
			Operation: v1.RelationshipUpdate_OPERATION_CREATE,
			Relationship: &v1.Relationship{
				Resource: &v1.ObjectReference{
					ObjectType: "mapmaker/map",
					ObjectId:   mapId,
				},
				Relation: "owner",
				Subject: &v1.SubjectReference{Object: &v1.ObjectReference{
					ObjectType: "mapmaker/player",
					ObjectId:   userId,
				}},
			},
		}},
	})
	if err != nil {
		return "", fmt.Errorf("failed to set map owner: %w", err)
	}
	return resp.WrittenAt.Token, nil
}

func (c *SpiceDBClient) DeleteMap(ctx context.Context, mapId string) error {
	_, err := c.sdb.DeleteRelationships(ctx, &v1.DeleteRelationshipsRequest{
		RelationshipFilter: &v1.RelationshipFilter{
			ResourceType:       "mapmaker/map",
			OptionalResourceId: mapId,
		},
	})
	if err != nil {
		return fmt.Errorf("failed to delete map: %w", err)
	}
	return nil
}

func (c *SpiceDBClient) PublishMap(ctx context.Context, mapId string) (string, error) {
	_, err := c.sdb.DeleteRelationships(ctx, &v1.DeleteRelationshipsRequest{
		RelationshipFilter: &v1.RelationshipFilter{
			ResourceType:       "mapmaker/map",
			OptionalResourceId: mapId,
			OptionalRelation:   "trusted",
		},
	})
	if err != nil {
		return "", fmt.Errorf("failed to remove trusted: %w", err)
	}

	_, err = c.sdb.DeleteRelationships(ctx, &v1.DeleteRelationshipsRequest{
		RelationshipFilter: &v1.RelationshipFilter{
			ResourceType:       "mapmaker/map",
			OptionalResourceId: mapId,
			OptionalRelation:   "owner",
		},
	})
	if err != nil {
		return "", fmt.Errorf("failed to remove owner: %w", err)
	}

	res, err := c.sdb.WriteRelationships(ctx, &v1.WriteRelationshipsRequest{
		Updates: []*v1.RelationshipUpdate{{
			Operation: v1.RelationshipUpdate_OPERATION_CREATE,
			Relationship: &v1.Relationship{
				Resource: &v1.ObjectReference{ObjectType: "mapmaker/map", ObjectId: mapId},
				Relation: "viewer",
				Subject:  &v1.SubjectReference{Object: &v1.ObjectReference{ObjectType: "mapmaker/player", ObjectId: "*"}},
			},
		}},
	})
	if err != nil {
		return "", fmt.Errorf("failed to add viewers: %w", err)
	}

	return res.WrittenAt.Token, nil
}

func (c *SpiceDBClient) CheckPlatformPermission(ctx context.Context, userId, cacheKey string, perm PlatformPermission) (State, error) {
	if _, ok := PlatformPermissionValidationMap[perm]; !ok {
		return Deny, fmt.Errorf("%w: %s", ErrNoSuchPermission, perm)
	}

	var consistency *v1.Consistency
	if cacheKey != "" {
		consistency = &v1.Consistency{Requirement: &v1.Consistency_AtLeastAsFresh{AtLeastAsFresh: &v1.ZedToken{Token: cacheKey}}}
	}

	caveatContext, err := structpb.NewStruct(map[string]interface{}{
		"never_set":    true, //todo audit log impl
		"current_time": time.Now().Format(time.RFC3339),
	})
	if err != nil {
		return Deny, fmt.Errorf("failed to create caveat context: %w", err)
	}

	resp, err := c.sdb.CheckPermission(ctx, &v1.CheckPermissionRequest{
		Consistency: consistency,
		Resource:    &v1.ObjectReference{ObjectType: "mapmaker/platform", ObjectId: "0"},
		Permission:  string(perm),
		Subject:     &v1.SubjectReference{Object: &v1.ObjectReference{ObjectType: "mapmaker/player", ObjectId: userId}},
		Context:     caveatContext,
	})
	if err != nil {
		return Deny, fmt.Errorf("failed to check permission: %w", err)
	}
	return stateFromSpiceDB(resp), nil
}

func (c *SpiceDBClient) CheckMapPermission(ctx context.Context, mapId, userId, cacheKey string, perm MapPermission) (State, error) {
	if _, ok := MapPermissionValidationMap[perm]; !ok {
		return Deny, fmt.Errorf("%w: %s", ErrNoSuchPermission, perm)
	}

	var consistency *v1.Consistency
	if cacheKey != "" {
		consistency = &v1.Consistency{Requirement: &v1.Consistency_AtLeastAsFresh{AtLeastAsFresh: &v1.ZedToken{Token: cacheKey}}}
	}

	resp, err := c.sdb.CheckPermission(ctx, &v1.CheckPermissionRequest{
		Consistency: consistency,
		Resource:    &v1.ObjectReference{ObjectType: "mapmaker/map", ObjectId: mapId},
		Permission:  string(perm),
		Subject:     &v1.SubjectReference{Object: &v1.ObjectReference{ObjectType: "mapmaker/player", ObjectId: userId}},
	})
	if err != nil {
		return Deny, fmt.Errorf("failed to check permission: %w", err)
	}
	return stateFromSpiceDB(resp), nil
}

func (c *SpiceDBClient) CheckMapRead(ctx context.Context, mapId, userId, cacheKey string) (bool, error) {
	state, err := c.CheckMapGeneric(ctx, mapId, userId, cacheKey, "read")
	return state == Allow, err
}

func (c *SpiceDBClient) CheckMapWrite(ctx context.Context, mapId, userId, cacheKey string) (bool, error) {
	state, err := c.CheckMapGeneric(ctx, mapId, userId, cacheKey, "write")
	return state == Allow, err
}

func (c *SpiceDBClient) CheckMapAdmin(ctx context.Context, mapId, userId, cacheKey string) (bool, error) {
	state, err := c.CheckMapGeneric(ctx, mapId, userId, cacheKey, "admin")
	return state == Allow, err
}

func (c *SpiceDBClient) CheckMapGeneric(ctx context.Context, mapId, userId, cacheKey, perm string) (State, error) {
	return Allow, nil
	//var consistenc/*y *v1.Consistency
	//if cacheKey != "" {
	//	consistency = &v1.Consistency{Requirement: &v1.Consistency_AtLeastAsFresh{AtLeastAsFresh: &v1.ZedToken{Token: cacheKey}}}
	//}
	//
	//resp, err := c.sdb.CheckPermission(ctx, &v1.CheckPermissionRequest{
	//	Consistency: consistency,
	//	Resource:    &v1.ObjectReference{ObjectType: "mapmaker/map", ObjectId: mapId},
	//	Permission:  perm,
	//	Subject:     &v1.SubjectReference{Object: &v1.ObjectReference{ObjectType: "mapmaker/player", ObjectId: userId}},
	//})
	//if err != nil {
	//	return Deny, fmt.Errorf("failed to check permission: %w", err)
	//}
	//return stateFromSpiceDB(resp), nil*/
}

func stateFromSpiceDB(resp *v1.CheckPermissionResponse) State {
	switch resp.Permissionship {
	case v1.CheckPermissionResponse_PERMISSIONSHIP_HAS_PERMISSION:
		return Allow
	case v1.CheckPermissionResponse_PERMISSIONSHIP_NO_PERMISSION:
		return Deny
	case v1.CheckPermissionResponse_PERMISSIONSHIP_UNSPECIFIED:
		return Unspecified
	default:
		return Deny
	}
}
