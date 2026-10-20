package xray

import (
	"context"
	"errors"
	"testing"
	"time"

	statsService "github.com/xtls/xray-core/app/stats/command"
	"google.golang.org/grpc"
)

type monitorStatsClient struct {
	statsService.StatsServiceClient
	query func(context.Context, *statsService.QueryStatsRequest) (*statsService.QueryStatsResponse, error)
}

func (c monitorStatsClient) QueryStats(ctx context.Context, request *statsService.QueryStatsRequest, _ ...grpc.CallOption) (*statsService.QueryStatsResponse, error) {
	return c.query(ctx, request)
}

func TestGetInboundTrafficDoesNotConsumeCounters(t *testing.T) {
	var client statsService.StatsServiceClient = monitorStatsClient{query: func(ctx context.Context, request *statsService.QueryStatsRequest) (*statsService.QueryStatsResponse, error) {
		if request.Reset_ || request.Pattern != "inbound>>>" {
			t.Fatalf("monitor must read only inbound counters without resetting: %+v", request)
		}
		if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) > 3*time.Second {
			t.Fatal("monitor requests must have a short deadline")
		}
		return &statsService.QueryStatsResponse{Stat: []*statsService.Stat{
			{Name: "inbound>>>inbound-443>>>traffic>>>uplink", Value: 100},
			{Name: "inbound>>>inbound-443>>>traffic>>>downlink", Value: 200},
			{Name: "inbound>>>api>>>traffic>>>uplink", Value: 1000},
			{Name: "outbound>>>direct>>>traffic>>>uplink", Value: 2000},
			{Name: "user>>>test@example.com>>>traffic>>>downlink", Value: 3000},
			{Name: "inbound>>>invalid>>>traffic>>>unknown", Value: 4000},
		}}, nil
	}}
	api := XrayAPI{grpcClient: new(grpc.ClientConn), StatsServiceClient: &client}
	traffic, err := api.GetInboundTraffic()
	if err != nil || len(traffic) != 1 {
		t.Fatalf("unexpected traffic: %+v, error: %v", traffic, err)
	}
	if got := traffic[0]; got.Tag != "inbound-443" || !got.IsInbound || got.Up != 100 || got.Down != 200 {
		t.Fatalf("incorrect inbound directions: %+v", got)
	}
}

func TestGetInboundTrafficErrors(t *testing.T) {
	api := XrayAPI{}
	if _, err := api.GetInboundTraffic(); err == nil {
		t.Fatal("uninitialized API must return an error")
	}
	expected := errors.New("stats unavailable")
	var client statsService.StatsServiceClient = monitorStatsClient{query: func(context.Context, *statsService.QueryStatsRequest) (*statsService.QueryStatsResponse, error) {
		return nil, expected
	}}
	api = XrayAPI{grpcClient: new(grpc.ClientConn), StatsServiceClient: &client}
	if _, err := api.GetInboundTraffic(); !errors.Is(err, expected) {
		t.Fatalf("expected query error, got %v", err)
	}
}
