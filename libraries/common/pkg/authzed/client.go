package authzedUtil

import (
	"context"
	"fmt"
	"strings"

	"github.com/authzed/authzed-go/v1"
	"github.com/authzed/grpcutil"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

var otelTracer = otel.Tracer("github.com/hollow-cube/hc-services/libraries/common/pkg/authzed")

func NewAuthzedClient(address string, token string, secure bool) (*authzed.Client, error) {
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
	return sdb, nil
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
