package dispatcher

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xtls/xray-core/common/buf"
	"github.com/xtls/xray-core/common/subscriptionrate"
)

type failingRateSink struct{ calls int }

func (w *failingRateSink) WriteMultiBuffer(mb buf.MultiBuffer) error {
	w.calls++
	buf.ReleaseMulti(mb)
	return io.ErrClosedPipe
}

func ownedRateBuffers(count int) (buf.MultiBuffer, []*buf.Buffer) {
	mb := make(buf.MultiBuffer, count)
	refs := make([]*buf.Buffer, count)
	for i := range mb {
		mb[i] = buf.New()
		mb[i].Extend(buf.Size)
		refs[i] = mb[i]
	}
	return mb, refs
}

func TestRateWriterReleasesRemainderAfterSinkError(t *testing.T) {
	mb, refs := ownedRateBuffers(3)
	sink := new(failingRateSink)
	writer := newRateWriter(context.Background(), sink, subscriptionrate.New(subscriptionrate.MaxRate, 0).Uplink)
	defer writer.Close()
	if err := writer.WriteMultiBuffer(mb); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("wrong error: %v", err)
	}
	if sink.calls != 1 {
		t.Fatal("continued writing after sink error")
	}
	for _, b := range refs {
		if b.Len() != 0 {
			t.Fatal("buffer retained after write failure")
		}
	}
}

func TestRateReaderCloseReleasesPendingAndPreventsNewReads(t *testing.T) {
	mb, refs := ownedRateBuffers(3)
	reader := newRateReader(context.Background(), &finalBufferReader{payload: mb}, subscriptionrate.New(subscriptionrate.MaxRate, 0).Uplink)
	first, err := reader.ReadMultiBuffer()
	if err != nil {
		t.Fatal(err)
	}
	buf.ReleaseMulti(first)
	reader.Close()
	reader.Close()
	for _, b := range refs {
		if b.Len() != 0 {
			t.Fatal("pending buffer retained after close")
		}
	}
	if mb, err := reader.ReadMultiBuffer(); !errors.Is(err, context.Canceled) || len(mb) != 0 {
		t.Fatal("closed reader returned data")
	}
}

type countingRateSink struct{ calls atomic.Int64 }

func (w *countingRateSink) WriteMultiBuffer(mb buf.MultiBuffer) error {
	w.calls.Add(1)
	buf.ReleaseMulti(mb)
	return nil
}

func TestRateWriterCancelDuringWait(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	limit := subscriptionrate.New(1, 0).Uplink
	limit.AllowN(time.Now(), subscriptionrate.Burst)
	sink := new(countingRateSink)
	writer := newRateWriter(ctx, sink, limit)
	defer writer.Close()
	mb, refs := ownedRateBuffers(2)
	done := make(chan error, 1)
	go func() { done <- writer.WriteMultiBuffer(mb) }()
	select {
	case err := <-done:
		t.Fatalf("exhausted slow budget did not block: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("wrong cancellation error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("reservation did not stop")
	}
	if sink.calls.Load() != 0 {
		t.Fatal("cancelled data escaped to sink")
	}
	for _, b := range refs {
		if b.Len() != 0 {
			t.Fatal("cancelled write retained buffer")
		}
	}
}

func TestRateReaderInterruptStress(t *testing.T) {
	for range 64 {
		limit := subscriptionrate.New(1, 0).Uplink
		limit.AllowN(time.Now(), subscriptionrate.Burst)
		mb, refs := ownedRateBuffers(2)
		reader := newRateReader(context.Background(), &finalBufferReader{payload: mb}, limit)
		done := make(chan struct{})
		go func() { mb, _ := reader.ReadMultiBuffer(); buf.ReleaseMulti(mb); close(done) }()
		reader.Interrupt()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("reader goroutine stuck")
		}
		// A source not read at all still owns its input; release its fixture data.
		reader.Close()
		if source := reader.reader.(*finalBufferReader); source.payload != nil {
			buf.ReleaseMulti(source.payload)
			source.payload = nil
		}
		for _, b := range refs {
			if b.Len() != 0 {
				t.Fatal("stress fixture retained data")
			}
		}
	}
}

func FuzzRateChunksPreservePayload(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte("small"))
	f.Add(bytes.Repeat([]byte{7}, 3*subscriptionrate.Burst+1))
	f.Fuzz(func(t *testing.T, payload []byte) {
		if len(payload) > 128*1024 {
			t.Skip()
		}
		mb := buf.MultiBuffer{nil, buf.FromBytes(payload), nil}
		var got []byte
		for steps := 0; len(mb) > 0; steps++ {
			if steps > 32 {
				buf.ReleaseMulti(mb)
				t.Fatal("chunker stopped progressing")
			}
			var chunk buf.MultiBuffer
			mb, chunk = takeRateChunk(mb)
			if chunk.Len() > subscriptionrate.Burst {
				buf.ReleaseMulti(chunk)
				buf.ReleaseMulti(mb)
				t.Fatal("oversized stream chunk")
			}
			got = append(got, []byte(chunk.String())...)
			buf.ReleaseMulti(chunk)
		}
		if !bytes.Equal(got, payload) {
			t.Fatal("chunker lost or duplicated bytes")
		}
	})
}
