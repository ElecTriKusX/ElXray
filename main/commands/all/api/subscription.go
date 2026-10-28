package api

import (
	"math"

	"google.golang.org/protobuf/proto"

	service "github.com/xtls/xray-core/app/policy/command"
	"github.com/xtls/xray-core/main/commands/base"
)

var cmdSubscriptionGet = &base.Command{
	CustomFlags: true,
	UsageLine:   "{{.Exec}} api subscription-get [--server=127.0.0.1:10085] [--id=lab-account]",
	Short:       "Read a configured subscription's rate policy",
	Run:         executeSubscriptionGet,
}

var cmdSubscriptionSet = &base.Command{
	CustomFlags: true,
	UsageLine:   "{{.Exec}} api subscription-set [--server=127.0.0.1:10085] [--id=lab-account] [--expected-revision=1] [--revision=2] [--uplink-bps=625000] [--downlink-bps=1250000] [--disabled=false]",
	Short:       "Update a local subscription policy without restarting connections",
	Long: `Both byte rates and disabled must be explicitly provided. Zero means unlimited.
Uses compare-and-swap revisions; identical retries are safe. Changes are in memory:
the supervisor must also persist the policy for the next start. Email bindings cannot
be changed at runtime. The API must listen on loopback or a protected Unix socket.`,
	Run: executeSubscriptionSet,
}

func executeSubscriptionGet(cmd *base.Command, args []string) {
	setSharedFlags(cmd)
	id := cmd.Flag.String("id", "", "")
	cmd.Flag.Parse(args)
	if *id == "" {
		base.Fatalf("id is required")
	}
	conn, ctx, close := dialAPIServer()
	defer close()
	resp, err := service.NewSubscriptionServiceClient(conn).GetSubscription(ctx, &service.GetSubscriptionRequest{Id: *id})
	if err != nil {
		base.Fatalf("failed to get subscription: %s", err)
	}
	showJSONResponse(resp)
}

func executeSubscriptionSet(cmd *base.Command, args []string) {
	setSharedFlags(cmd)
	id := cmd.Flag.String("id", "", "")
	expected := cmd.Flag.Uint64("expected-revision", 0, "")
	revision := cmd.Flag.Uint64("revision", 0, "")
	up := cmd.Flag.Uint64("uplink-bps", math.MaxUint64, "")
	down := cmd.Flag.Uint64("downlink-bps", math.MaxUint64, "")
	disabled := cmd.Flag.String("disabled", "", "")
	cmd.Flag.Parse(args)
	if *id == "" || *expected == 0 || *revision == 0 || *up == math.MaxUint64 || *down == math.MaxUint64 || (*disabled != "true" && *disabled != "false") {
		base.Fatalf("id, expected-revision, revision, uplink-bps, downlink-bps and disabled=true|false must be explicit")
	}
	conn, ctx, close := dialAPIServer()
	defer close()
	resp, err := service.NewSubscriptionServiceClient(conn).SetSubscription(ctx, &service.SetSubscriptionRequest{Id: *id, ExpectedRevision: *expected, Revision: *revision, UplinkBps: up, DownlinkBps: down, Disabled: proto.Bool(*disabled == "true")})
	if err != nil {
		base.Fatalf("failed to update subscription: %s", err)
	}
	showJSONResponse(resp)
}
