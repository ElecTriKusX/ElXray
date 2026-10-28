package proxy_test

import (
	"bytes"
	"context"
	"net"
	"testing"
	"time"

	"github.com/xtls/xray-core/common/buf"
	"github.com/xtls/xray-core/common/session"
	"github.com/xtls/xray-core/common/signal"
	"github.com/xtls/xray-core/proxy"
)

type guardedWriter struct{ buf.Writer }

func (guardedWriter) RequiresBufferedCopy() bool { return true }

func tcpPair(t *testing.T) (*net.TCPConn, *net.TCPConn) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	client, err := net.DialTimeout("tcp", listener.Addr().String(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	server, err := listener.Accept()
	if err != nil {
		client.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { client.Close(); server.Close() })
	deadline := time.Now().Add(5 * time.Second)
	client.SetDeadline(deadline)
	server.SetDeadline(deadline)
	return client.(*net.TCPConn), server.(*net.TCPConn)
}

func TestRawCopyCannotBypassGuardedWriter(t *testing.T) {
	sender, source := tcpPair(t)
	destination, _ := tcpPair(t)
	payload := []byte("subscription budget must observe these bytes")
	go func() { sender.Write(payload); sender.CloseWrite() }()
	ctx := session.ContextWithInbound(context.Background(), &session.Inbound{CanSpliceCopy: 1})
	ctx = session.ContextWithOutbounds(ctx, []*session.Outbound{{CanSpliceCopy: 1}})
	timer := signal.CancelAfterInactivity(ctx, func() {}, time.Minute)
	defer timer.SetTimeout(0)
	var output bytes.Buffer
	writer := guardedWriter{Writer: buf.NewWriter(&output)}
	if err := proxy.CopyRawConnIfExist(ctx, source, destination, writer, timer, nil); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(output.Bytes(), payload) {
		t.Fatal("raw socket copy bypassed the guarded writer")
	}
}
