package authz

import (
	"context"
	"fmt"

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

func (c *SpiceDBClient) CheckPlatformPermission(ctx context.Context, userId, cacheKey string, perm PlatformPermission) (State, error) {
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
