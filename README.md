# ElXray

ElXray is an [XTLS/Xray-core](https://github.com/XTLS/Xray-core) fork with per-subscription upload/download limits and a local gRPC API for updating policies on active connections. Licensed under [MPL-2.0](LICENSE); upstream copyright notices are preserved.

## Features

- Shared byte budgets across a subscription's server-side email identities, connections and source IPs.
- Independent upload and download limits within each core instance.
- Live rate updates, including unlimited-to-limited transitions, without restarting connections.
- Revision-based compare-and-swap, idempotent retries and last-known-good state on rejected updates.
- Subscription blocking with `disabled` and optional fail-closed identity bindings with `requireSubscription`.
- Buffered-copy guards prevent raw socket splice from bypassing managed budgets, including Vision and statistics wrappers.
- Synchronized TLS certificate reload and immutable certificate publication during handshakes.

Limits are local to one process/node. They count dispatcher bytes rather than all wire overhead. Equal bandwidth shares between connections are not guaranteed. These policies do not identify physical devices.

## Build and run

Use the Go version specified in [go.mod](go.mod), or a compatible newer version.

Windows:

```powershell
go build -o elxray.exe ./main
.\elxray.exe version
.\elxray.exe run -config config.json
```

Linux/macOS:

```sh
go build -o elxray ./main
./elxray run -config config.json
```

The Go module path `github.com/xtls/xray-core` and protobuf namespaces remain unchanged for source and SDK compatibility. Product binaries are named `elxray` / `elxray.exe`. Process managers may require a different executable filename.

## Subscription policies

Merge this fragment into the existing server configuration, preserving other policy settings:

```json
{
  "policy": {
    "requireSubscription": true,
    "subscriptions": {
      "account-123": {
        "emails": ["account-123@example"],
        "uplinkBps": 625000,
        "downlinkBps": 1250000,
        "revision": 1,
        "disabled": false
      }
    }
  }
}
```

Emails must match authenticated **server-side** user identities. They are not supplied by HTTP request headers. Matching is case-insensitive; duplicate bindings are rejected.

`Bps` means **bytes per second**: Mbps × 1,000,000 / 8. This example allows 5 Mbps upload and 10 Mbps download from the client's perspective, simultaneously. Set both values to `12500000` for 100 Mbps in each direction; there is no combined upload-plus-download budget.

A zero rate means unlimited in that direction. `disabled: true` blocks both directions. `requireSubscription: true` blocks authenticated users without a configured binding, including empty emails. Without this flag, unbound identities retain upstream behavior.

Initial burst credit is 8192 bytes; the maximum configured rate is 2^40 bytes/s per direction. Updates preserve the budget instead of granting fresh burst credit. UDP datagrams remain intact: average-rate pacing is not strict packet policing.

## Management API

Xray's existing Commander exposes gRPC services such as HandlerService and StatsService. ElXray adds **SubscriptionService** to the same API infrastructure; it is not an HTTP REST endpoint.

Add this fragment to enable the local services:

```json
"api": {
  "tag": "api",
  "listen": "127.0.0.1:10085",
  "services": ["StatsService", "SubscriptionService"]
}
```

SubscriptionService requires an explicit loopback listener or a protected Unix socket. Local processes are trusted: Commander does not authenticate local users by password. Do not expose the management endpoint to the LAN or Internet.

```powershell
.\elxray.exe api subscription-get --server=127.0.0.1:10085 --id=account-123
.\elxray.exe api subscription-set --server=127.0.0.1:10085 --id=account-123 --expected-revision=1 --revision=2 --uplink-bps=312500 --downlink-bps=625000 --disabled=false
```

Both rates and `disabled` must be explicit. The new revision equals expected-revision + 1. Conflicting stale requests are rejected; exact retries of accepted updates are safe.

**Runtime changes are in memory.** The managing process must persist policies and generate startup JSON with the current rates, revision and disabled state. The core does not rewrite configuration files. Creating or changing email bindings requires a configuration update and restart. Disabled is not UUID revocation and does not guarantee immediate closure of every idle socket.

Additional reference: [runtime API](docs/electrium/RUNTIME_API.md) and [rate semantics](docs/electrium/SUBSCRIPTION_RATE_LIMIT.md).

## Testing

```powershell
go test -race ./common/subscriptionrate ./app/dispatcher ./app/policy ./app/policy/command ./common/buf ./proxy ./testing/subscriptionrate -count=1 -timeout 120s
go test -race ./infra/conf -run '^TestSubscription' -count=1 -timeout 90s
go test -race ./transport/internet/tls -run '^TestCertificate' -count=1 -timeout 30s
python3 -m unittest discover -s testing/subscriptionrate -p 'test_*.py' -v
python3 testing/subscriptionrate/local_smoke.py --xray .\elxray.exe
```

Race detection requires a supported C toolchain. The full `go test ./...` suite needs `resources/geoip.dat` and `resources/geosite.dat`; some upstream tests also access external services. CI downloads assets with SHA-256 verification and transfers them through workflow artifacts. Windows 7 packaging uses a separate patched toolchain; successful packaging is not a test on physical Windows 7 hardware.

The smoke runner starts a loopback HTTP fixture and two core processes with a temporary UUID, checks shared bidirectional limits and runtime retry/conflict/disable/unlimited behavior, then stops its own processes. Free lab ports 1080, 18080, 24443 and 10085 before running it.

Network regression tests cover VLESS TCP, UDP, UDP mux, XUDP, TCP mux and Vision over TLS/REALITY with a local TLS target. They do not certify every transport combination or sustained-load performance.

The benchmark's `--mode tun` option requires an existing TUN interface and route; it does not create one. See the [single-PC and two-device lab guide](testing/subscriptionrate/README.md).
