package subscriptionrate_test

import (
	"bytes"
	"fmt"
	"io"
	stdnet "net"
	"sync"
	"testing"
	"time"

	"github.com/xtls/xray-core/app/dispatcher"
	"github.com/xtls/xray-core/app/policy"
	"github.com/xtls/xray-core/app/proxyman"
	"github.com/xtls/xray-core/common/net"
	"github.com/xtls/xray-core/common/protocol"
	"github.com/xtls/xray-core/common/serial"
	"github.com/xtls/xray-core/common/uuid"
	"github.com/xtls/xray-core/core"
	_ "github.com/xtls/xray-core/main/distro/all"
	"github.com/xtls/xray-core/proxy/freedom"
	"github.com/xtls/xray-core/proxy/vless"
	"github.com/xtls/xray-core/proxy/vless/encoding"
	"github.com/xtls/xray-core/proxy/vless/inbound"
	"github.com/xtls/xray-core/testing/servers/tcp"
)

// This is an actual VLESS TCP exchange on loopback, not a physical-device or
// REALITY/Vision test. No listener is exposed to external interfaces.
func TestSubscriptionVLESSSharedRate(t *testing.T) {
	for _, direction := range []string{"uplink", "downlink"} {
		t.Run(direction, func(t *testing.T) {
			target, err := stdnet.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { target.Close() })
			go func() {
				for {
					conn, err := target.Accept()
					if err != nil {
						return
					}
					go func() { defer conn.Close(); conn.SetDeadline(time.Now().Add(10 * time.Second)); io.Copy(conn, conn) }()
				}
			}()
			id := uuid.New()
			port := tcp.PickPort()
			const speed = 256 * 1024
			budget := &policy.SubscriptionPolicy{Emails: []string{"account@example"}}
			if direction == "uplink" {
				budget.UplinkBps = speed
			} else {
				budget.DownlinkBps = speed
			}
			instance, err := core.New(&core.Config{
				App: []*serial.TypedMessage{
					serial.ToTypedMessage(&dispatcher.Config{}),
					serial.ToTypedMessage(&proxyman.InboundConfig{}),
					serial.ToTypedMessage(&proxyman.OutboundConfig{}),
					serial.ToTypedMessage(&policy.Config{Subscriptions: map[string]*policy.SubscriptionPolicy{"account": budget}}),
				},
				Inbound: []*core.InboundHandlerConfig{{
					ReceiverSettings: serial.ToTypedMessage(&proxyman.ReceiverConfig{Listen: net.NewIPOrDomain(net.LocalHostIP), PortList: &net.PortList{Range: []*net.PortRange{net.SinglePortRange(port)}}}),
					ProxySettings:    serial.ToTypedMessage(&inbound.Config{Users: []*protocol.User{{Email: "ACCOUNT@EXAMPLE", Account: serial.ToTypedMessage(&vless.Account{Id: id.String()})}}}),
				}},
				Outbound: []*core.OutboundHandlerConfig{{ProxySettings: serial.ToTypedMessage(&freedom.Config{FinalRules: []*freedom.FinalRuleConfig{{Action: freedom.RuleAction_Allow}}})}},
			})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { instance.Close() })
			if err := instance.Start(); err != nil {
				t.Fatal(err)
			}
			dest := net.DestinationFromAddr(target.Addr())
			dial := func(userID uuid.UUID) (stdnet.Conn, *protocol.RequestHeader, error) {
				conn, err := stdnet.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), time.Second)
				if err != nil {
					return nil, nil, err
				}
				conn.SetDeadline(time.Now().Add(5 * time.Second))
				request := &protocol.RequestHeader{Version: 0, Command: protocol.RequestCommandTCP, Address: dest.Address, Port: dest.Port, User: &protocol.MemoryUser{Email: "client-cannot-override-server-email", Account: &vless.MemoryAccount{ID: protocol.NewID(userID)}}}
				if err := encoding.EncodeRequestHeader(conn, request, &encoding.Addons{}); err != nil {
					conn.Close()
					return nil, nil, err
				}
				return conn, request, nil
			}
			// Reopening connections never allocates a new subscription bucket.
			for _, connections := range []int{1, 2} {
				payload := bytes.Repeat([]byte{17}, 64*1024)
				start := time.Now()
				var wg sync.WaitGroup
				for range connections {
					wg.Go(func() {
						conn, request, err := dial(id)
						if err != nil {
							t.Error(err)
							return
						}
						defer conn.Close()
						sent := make(chan error, 1)
						go func() { _, err := io.Copy(conn, bytes.NewReader(payload)); sent <- err }()
						if _, err := encoding.DecodeResponseHeader(conn, request); err != nil {
							t.Error(err)
							return
						}
						got := make([]byte, len(payload))
						if _, err := io.ReadFull(conn, got); err != nil {
							t.Error(err)
							return
						}
						if err := <-sent; err != nil {
							t.Error(err)
						}
						if !bytes.Equal(got, payload) {
							t.Error("payload corrupted")
						}
					})
				}
				wg.Wait()
				floor := time.Duration(float64(connections*len(payload)-8192) / speed * float64(time.Second) * 0.7)
				if elapsed := time.Since(start); elapsed < floor {
					t.Fatalf("%d connections bypassed %s budget: %v < %v", connections, direction, elapsed, floor)
				}
			}
			wrong := id
			wrong[15] ^= 1
			conn, request, err := dial(wrong)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			conn.Write([]byte{1})
			if _, err := encoding.DecodeResponseHeader(conn, request); err == nil {
				t.Fatal("unknown UUID authenticated")
			}
		})
	}
}
