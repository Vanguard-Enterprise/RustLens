package observability_test

import (
	"context"
	"net"
	"testing"
	"time"

	coltracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"google.golang.org/grpc"

	"rustlens/backend/internal/config"
	"rustlens/backend/internal/observability"

	"go.opentelemetry.io/otel"
)

type fakeTraceServer struct {
	coltracepb.UnimplementedTraceServiceServer
	got chan struct{}
}

func (f *fakeTraceServer) Export(_ context.Context, req *coltracepb.ExportTraceServiceRequest) (*coltracepb.ExportTraceServiceResponse, error) {
	select {
	case f.got <- struct{}{}:
	default:
	}
	return &coltracepb.ExportTraceServiceResponse{}, nil
}

func TestInitEmitsSpansToReceiver(t *testing.T) {
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer()
	fake := &fakeTraceServer{got: make(chan struct{}, 1)}
	coltracepb.RegisterTraceServiceServer(server, fake)
	go server.Serve(lis)
	t.Cleanup(server.Stop)

	cfg := config.Config{
		ObservabilityEnabled: true,
		OTLPEndpoint:         "http://" + lis.Addr().String(),
		ServiceName:          "rustlens-backend-test",
		ServiceNamespace:     "rustlens",
		DeploymentEnv:        "test",
		TraceSampleRatio:     1.0,
	}
	shutdown, err := observability.Init(context.Background(), cfg)
	if err != nil {
		t.Fatalf("init: %v", err)
	}
	t.Cleanup(func() { _ = shutdown(context.Background()) })

	ctx, span := otel.Tracer("smoke").Start(context.Background(), "smoke-span")
	span.End()
	_ = ctx

	select {
	case <-fake.got:
		// success
	case <-time.After(15 * time.Second):
		t.Fatal("no span received within timeout")
	}
}
