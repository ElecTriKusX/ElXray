package dispatcher

import (
	"bytes"
	"context"
	"io"
	"sync"
	"testing"
	"time"

	apppolicy "github.com/xtls/xray-core/app/policy"
	"github.com/xtls/xray-core/common"
	"github.com/xtls/xray-core/common/buf"
	"github.com/xtls/xray-core/common/net"
	"github.com/xtls/xray-core/common/protocol"
	"github.com/xtls/xray-core/common/session"
	"github.com/xtls/xray-core/common/subscriptionrate"
	"github.com/xtls/xray-core/transport"
)

func TestRateWriterSharesBudgetAcrossConnections(t *testing.T) {
	limits := subscriptionrate.New(128*1024, 0)
	var wg sync.WaitGroup
	start := time.Now()
	for range 2 {
		wg.Go(func() {
			var output buf.MultiBufferContainer
			defer output.Close()
			writer := newRateWriter(context.Background(), &output, limits.Uplink)
			defer writer.Close()
			payload := bytes.Repeat([]byte{42}, 16*1024)
			// A single buffer bigger than the burst must also be accepted.
			if err := writer.WriteMultiBuffer(buf.MultiBuffer{buf.FromBytes(payload)}); err != nil {
				t.Error(err)
				return
			}
			if !bytes.Equal([]byte(output.MultiBuffer.String()), payload) {
				t.Error("payload changed")
			}
		})
	}
	wg.Wait()
	if elapsed := time.Since(start); elapsed < 140*time.Millisecond {
		t.Fatalf("connections bypassed shared rate: %s", elapsed)
	}
}

func TestRateWriterInterruptCancelsWait(t *testing.T) {
	limits := subscriptionrate.New(1, 0)
	limits.Uplink.AllowN(time.Now(), subscriptionrate.Burst)
	var output buf.MultiBufferContainer
	defer output.Close()
	writer := newRateWriter(context.Background(), &output, limits.Uplink)
	done := make(chan error, 1)
	go func() { done <- writer.WriteMultiBuffer(buf.MultiBuffer{buf.FromBytes([]byte{1})}) }()
	writer.Interrupt()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancelled write succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("interrupt did not cancel rate wait")
	}
	if !output.MultiBuffer.IsEmpty() {
		t.Fatal("cancelled payload reached destination")
	}
}

func TestRateReaderChunksAndPreservesEOF(t *testing.T) {
	payload := bytes.Repeat([]byte{7}, 3*subscriptionrate.Burst)
	reader := newRateReader(context.Background(), buf.NewReader(bytes.NewReader(payload)), subscriptionrate.New(1024*1024, 0).Uplink)
	defer reader.Close()
	var output []byte
	for {
		mb, err := reader.ReadMultiBuffer()
		if mb.Len() > subscriptionrate.Burst {
			t.Fatal("oversized paced read")
		}
		output = append(output, []byte(mb.String())...)
		buf.ReleaseMulti(mb)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	if !bytes.Equal(output, payload) {
		t.Fatal("reader lost data")
	}
}

type finalBufferReader struct{ payload buf.MultiBuffer }

func (r *finalBufferReader) ReadMultiBuffer() (buf.MultiBuffer, error) {
	mb := r.payload
	r.payload = nil
	return mb, io.EOF
}

func TestRateReaderDefersEOFUntilLastChunk(t *testing.T) {
	payload := bytes.Repeat([]byte{3}, 3*subscriptionrate.Burst)
	reader := newRateReader(context.Background(), &finalBufferReader{payload: buf.MultiBuffer{buf.FromBytes(payload)}}, subscriptionrate.New(1024*1024, 0).Uplink)
	defer reader.Close()
	var output []byte
	for i := range 3 {
		mb, err := reader.ReadMultiBuffer()
		output = append(output, []byte(mb.String())...)
		buf.ReleaseMulti(mb)
		if i < 2 && err != nil {
			t.Fatal("EOF before buffered data drained")
		}
		if i == 2 && err != io.EOF {
			t.Fatal("final EOF lost")
		}
	}
	if !bytes.Equal(output, payload) {
		t.Fatal("buffered EOF data lost")
	}
}

func TestRateReaderInterruptThroughTimeoutWrapper(t *testing.T) {
	limits := subscriptionrate.New(1, 0)
	limits.Uplink.AllowN(time.Now(), subscriptionrate.Burst)
	reader := newRateReader(context.Background(), buf.NewReader(bytes.NewReader([]byte{1})), limits.Uplink)
	wrapper := &buf.TimeoutWrapperReader{Reader: reader}
	done := make(chan error, 1)
	go func() { mb, err := wrapper.ReadMultiBuffer(); buf.ReleaseMulti(mb); done <- err }()
	common.Interrupt(wrapper)
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("interrupted read succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("timeout wrapper swallowed interrupt")
	}
}

func TestRateChunkPreservesDatagram(t *testing.T) {
	dest := net.UDPDestination(net.LocalHostIP, 53)
	b := buf.FromBytes(bytes.Repeat([]byte{9}, subscriptionrate.Burst+1))
	b.UDP = &dest
	rest, chunk := takeRateChunk(buf.MultiBuffer{b})
	defer buf.ReleaseMulti(chunk)
	if len(rest) != 0 || len(chunk) != 1 || chunk[0].UDP != &dest || chunk.Len() != subscriptionrate.Burst+1 {
		t.Fatal("UDP datagram split or metadata lost")
	}
}

func TestBothDispatcherPathsApplySubscriptionPolicy(t *testing.T) {
	manager, err := apppolicy.New(context.Background(), &apppolicy.Config{Subscriptions: map[string]*apppolicy.SubscriptionPolicy{
		"account": {Emails: []string{"user"}, UplinkBps: 1000, DownlinkBps: 2000},
	}})
	if err != nil {
		t.Fatal(err)
	}
	ctx := session.ContextWithInbound(context.Background(), &session.Inbound{User: &protocol.MemoryUser{Email: "user"}})
	d := &DefaultDispatcher{policy: manager}
	inbound, outbound := d.getLink(ctx)
	defer common.Interrupt(inbound.Reader)
	defer common.Interrupt(outbound.Reader)
	defer common.Close(inbound.Writer)
	defer common.Close(outbound.Writer)
	up, upOK := inbound.Writer.(*RateWriter)
	down, downOK := outbound.Writer.(*RateWriter)
	if !upOK || !downOK {
		t.Fatal("Dispatch path unprotected")
	}
	if up.limiter != manager.ForEmail("user").Uplink || down.limiter != manager.ForEmail("user").Downlink {
		t.Fatal("wrong direction budgets")
	}
	link := WrapLink(ctx, manager, nil, &transport.Link{Reader: buf.NewReader(bytes.NewReader(nil)), Writer: buf.Discard})
	reader := link.Reader.(*buf.TimeoutWrapperReader)
	limitedReader, ok := reader.Reader.(*rateReader)
	limitedWriter, writerOK := link.Writer.(*RateWriter)
	if !ok || !writerOK {
		t.Fatal("DispatchLink/VLESS path unprotected")
	}
	defer limitedReader.Close()
	defer limitedWriter.Close()
	if limitedReader.limiter != up.limiter || limitedWriter.limiter != down.limiter {
		t.Fatal("paths do not share subscription budget")
	}
	unlimited := WrapLink(context.Background(), manager, nil, &transport.Link{Reader: buf.NewReader(bytes.NewReader(nil)), Writer: buf.Discard})
	if _, ok := unlimited.Writer.(*RateWriter); ok {
		t.Fatal("unidentified traffic unexpectedly throttled")
	}
}
