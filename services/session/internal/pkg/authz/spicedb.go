package authz

import (
	"context"
	"fmt"
	"strings"
	"time"

	v1 "github.com/authzed/authzed-go/proto/authzed/api/v1"
	"github.com/authzed/authzed-go/v1"
	"github.com/authzed/grpcutil"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/structpb"
)

var _ Client = (*SpiceDBClient)(nil)
var otelTracer = otel.Tracer("github.com/hollow-cube/hc-services/services/session/internal/pkg/authz")

type SpiceDBClient struct {
	sdb *authzed.Client
}

func NewSpiceDBClient(address string, token string, secure bool) (*SpiceDBClient, error) {
	if secure {
		panic("secure spicedb connection not implemented")
	}

	var sdb *authzed.Client
	var err error

	tracingInterceptor := grpc.WithUnaryInterceptor(tracingUnaryInterceptor)

	sdb, err = authzed.NewClient(address,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpcutil.WithInsecureBearerToken(token),
		tracingInterceptor,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create SpiceDB client: %w", err)
	}
	return NewSpiceDBClientFromClient(sdb), nil
}

func NewSpiceDBClientFromClient(client *authzed.Client) *SpiceDBClient {
	return &SpiceDBClient{client}
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

func (c *SpiceDBClient) HasHypercube(ctx context.Context, playerId, cacheKey string) (bool, error) {
	var consistency *v1.Consistency
	if cacheKey != "" {
		consistency = &v1.Consistency{Requirement: &v1.Consistency_AtLeastAsFresh{AtLeastAsFresh: &v1.ZedToken{Token: cacheKey}}}
	}

	caveatContext, err := structpb.NewStruct(map[string]interface{}{
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

// tracingUnaryInterceptor is a gRPC unary interceptor that adds OpenTelemetry tracing for outgoing client requests.
// This could be generically adapted, but only for authzed for now.
func tracingUnaryInterceptor(
	ctx context.Context,
	method string,
	req, reply interface{},
	cc *grpc.ClientConn,
	invoker grpc.UnaryInvoker,
	opts ...grpc.CallOption,
) error {
	traceName := "authz.PermissionService." + strings.TrimPrefix(method, "/authzed.api.v1.PermissionsService/")
	ctx, span := otelTracer.Start(ctx, traceName, trace.WithSpanKind(trace.SpanKindClient))
	defer span.End()

	span.SetAttributes(
		attribute.String("rpc.service", "authzed.api.v1.PermissionsService"),
		attribute.String("rpc.method", method),
		attribute.String("server.address", cc.Target()),
	)

	// we have to invoke the request :)
	err := invoker(ctx, method, req, reply, cc, opts...)

	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	} else {
		span.SetStatus(codes.Ok, "")
	}

	return err
}
