package subscriptionrate_test

import (
	"bytes"
	"context"
	"crypto/ecdh"
	"crypto/rand"
	stdtls "crypto/tls"
	"crypto/x509"
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
	"github.com/xtls/xray-core/common/protocol/tls/cert"
	"github.com/xtls/xray-core/common/serial"
	"github.com/xtls/xray-core/common/subscriptionrate"
	"github.com/xtls/xray-core/common/uuid"
	"github.com/xtls/xray-core/core"
	featurepolicy "github.com/xtls/xray-core/features/policy"
	"github.com/xtls/xray-core/proxy/dokodemo"
	"github.com/xtls/xray-core/proxy/freedom"
	"github.com/xtls/xray-core/proxy/vless"
	"github.com/xtls/xray-core/proxy/vless/inbound"
	"github.com/xtls/xray-core/proxy/vless/outbound"
	"github.com/xtls/xray-core/testing/servers/tcp"
	"github.com/xtls/xray-core/transport/internet"
	"github.com/xtls/xray-core/transport/internet/reality"
	xtls "github.com/xtls/xray-core/transport/internet/tls"
)

// Both the REALITY target and the TLS application are local fixtures. A genuine
// inner TLS 1.3 stream exercises Vision's transition to direct copying; policies
// are enabled after that stream is already established and initially unlimited.
func TestSubscriptionVisionRuntime(t *testing.T) {
	for _, security := range []string{"tls", "reality"} {
		t.Run(security, func(t *testing.T) {
			certificate, hash := cert.MustGenerate(nil, cert.CommonName("localhost"), cert.DNSNames("localhost"))
			pemCert, pemKey := certificate.ToPEM()
			pair, err := stdtls.X509KeyPair(pemCert, pemKey)
			if err != nil {
				t.Fatal(err)
			}
			target, err := stdtls.Listen("tcp", "127.0.0.1:0", &stdtls.Config{Certificates: []stdtls.Certificate{pair}, MinVersion: stdtls.VersionTLS13})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { target.Close() })
			go func() {
				for {
					c, err := target.Accept()
					if err != nil {
						return
					}
					go func() { defer c.Close(); c.SetDeadline(time.Now().Add(15 * time.Second)); io.Copy(c, c) }()
				}
			}()
			serverPort, clientPort := tcp.PickPort(), tcp.PickPort()
			uid := uuid.New()
			id := uid.String()
			serverStream := &internet.StreamConfig{ProtocolName: "tcp", SecurityType: serial.GetMessageType(&xtls.Config{}), SecuritySettings: []*serial.TypedMessage{serial.ToTypedMessage(&xtls.Config{Certificate: []*xtls.Certificate{xtls.ParseCertificate(certificate)}})}}
			clientStream := &internet.StreamConfig{ProtocolName: "tcp", SecurityType: serial.GetMessageType(&xtls.Config{}), SecuritySettings: []*serial.TypedMessage{serial.ToTypedMessage(&xtls.Config{PinnedPeerCertSha256: [][]byte{hash[:]}})}}
			if security == "reality" {
				key, err := ecdh.X25519().GenerateKey(rand.Reader)
				if err != nil {
					t.Fatal(err)
				}
				short := []byte{1, 2, 3, 4, 5, 6, 7, 8}
				serverStream.SecurityType = serial.GetMessageType(&reality.Config{})
				serverStream.SecuritySettings = []*serial.TypedMessage{serial.ToTypedMessage(&reality.Config{Dest: target.Addr().String(), Type: "tcp", ServerNames: []string{"localhost"}, PrivateKey: key.Bytes(), ShortIds: [][]byte{short}})}
				clientStream.SecurityType = serial.GetMessageType(&reality.Config{})
				clientStream.SecuritySettings = []*serial.TypedMessage{serial.ToTypedMessage(&reality.Config{ServerName: "localhost", Fingerprint: "chrome", PublicKey: key.PublicKey().Bytes(), ShortId: short})}
			}
			user := func() *protocol.User {
				return &protocol.User{Email: "account@example", Account: serial.ToTypedMessage(&vless.Account{Id: id, Flow: vless.XRV})}
			}
			receiver := func(port net.Port, stream *internet.StreamConfig) *serial.TypedMessage {
				return serial.ToTypedMessage(&proxyman.ReceiverConfig{Listen: net.NewIPOrDomain(net.LocalHostIP), PortList: &net.PortList{Range: []*net.PortRange{net.SinglePortRange(port)}}, StreamSettings: stream})
			}
			server, err := core.New(&core.Config{
				App:      []*serial.TypedMessage{serial.ToTypedMessage(&dispatcher.Config{}), serial.ToTypedMessage(&proxyman.InboundConfig{}), serial.ToTypedMessage(&proxyman.OutboundConfig{}), serial.ToTypedMessage(&policy.Config{Subscriptions: map[string]*policy.SubscriptionPolicy{"account": {Emails: []string{"account@example"}}}})},
				Inbound:  []*core.InboundHandlerConfig{{ReceiverSettings: receiver(serverPort, serverStream), ProxySettings: serial.ToTypedMessage(&inbound.Config{Users: []*protocol.User{user()}})}},
				Outbound: []*core.OutboundHandlerConfig{{ProxySettings: serial.ToTypedMessage(&freedom.Config{FinalRules: []*freedom.FinalRuleConfig{{Action: freedom.RuleAction_Allow}}})}},
			})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { server.Close() })
			if err := server.Start(); err != nil {
				t.Fatal(err)
			}
			dest := net.DestinationFromAddr(target.Addr())
			client, err := core.New(&core.Config{
				App:      []*serial.TypedMessage{serial.ToTypedMessage(&dispatcher.Config{}), serial.ToTypedMessage(&proxyman.InboundConfig{}), serial.ToTypedMessage(&proxyman.OutboundConfig{})},
				Inbound:  []*core.InboundHandlerConfig{{ReceiverSettings: receiver(clientPort, nil), ProxySettings: serial.ToTypedMessage(&dokodemo.Config{RewriteAddress: net.NewIPOrDomain(dest.Address), RewritePort: uint32(dest.Port), AllowedNetworks: []net.Network{net.Network_TCP}})}},
				Outbound: []*core.OutboundHandlerConfig{{ProxySettings: serial.ToTypedMessage(&outbound.Config{Vnext: &protocol.ServerEndpoint{Address: net.NewIPOrDomain(net.LocalHostIP), Port: uint32(serverPort), User: user()}}), SenderSettings: serial.ToTypedMessage(&proxyman.SenderConfig{StreamSettings: clientStream})}},
			})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { client.Close() })
			if err := client.Start(); err != nil {
				t.Fatal(err)
			}
			pool := x509.NewCertPool()
			if !pool.AppendCertsFromPEM(pemCert) {
				t.Fatal("test CA")
			}
			var conns []stdnet.Conn
			for range 2 {
				raw, err := stdnet.DialTimeout("tcp", net.TCPDestination(net.LocalHostIP, clientPort).NetAddr(), time.Second)
				if err != nil {
					t.Fatal(err)
				}
				raw.SetDeadline(time.Now().Add(10 * time.Second))
				c := stdtls.Client(raw, &stdtls.Config{RootCAs: pool, ServerName: "localhost", MinVersion: stdtls.VersionTLS13})
				t.Cleanup(func() { c.Close() })
				if err := c.HandshakeContext(context.Background()); err != nil {
					t.Fatal(err)
				}
				// A full TLS application record establishes the direct-copy state.
				warm := bytes.Repeat([]byte{3}, 32768)
				done := make(chan error, 1)
				go func() { _, err := c.Write(warm); done <- err }()
				got := make([]byte, len(warm))
				if _, err := io.ReadFull(c, got); err != nil {
					t.Fatal(err)
				}
				if err := <-done; err != nil {
					t.Fatal(err)
				}
				conns = append(conns, c)
			}
			controller := server.GetFeature(featurepolicy.ManagerType()).(featurepolicy.SubscriptionController)
			if _, err := controller.UpdateSubscription(subscriptionrate.Update{ID: "account", ExpectedRevision: 1, Revision: 2, UplinkBps: 256 * 1024, DownlinkBps: 256 * 1024}); err != nil {
				t.Fatal(err)
			}
			start := time.Now()
			var wg sync.WaitGroup
			for _, c := range conns {
				wg.Go(func() {
					payload := bytes.Repeat([]byte{77}, 128*1024)
					done := make(chan error, 1)
					go func() { _, err := c.Write(payload); done <- err }()
					got := make([]byte, len(payload))
					if _, err := io.ReadFull(c, got); err != nil {
						t.Error(err)
						return
					}
					if err := <-done; err != nil {
						t.Error(err)
					}
					if !bytes.Equal(got, payload) {
						t.Error("TLS payload corrupted")
					}
				})
			}
			wg.Wait()
			if elapsed := time.Since(start); elapsed < 700*time.Millisecond {
				t.Fatalf("existing Vision connections bypassed runtime policy: %v", elapsed)
			}
		})
	}
}
