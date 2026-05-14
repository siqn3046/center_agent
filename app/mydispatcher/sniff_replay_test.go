package mydispatcher

import (
	"context"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/xtls/xray-core/common"
	"github.com/xtls/xray-core/common/buf"
)

// sliceBufReader returns fixed MultiBuffers in order (simulates TLS stream chunks).
type sliceBufReader struct {
	mu     sync.Mutex
	chunks []buf.MultiBuffer
}

func (s *sliceBufReader) ReadMultiBuffer() (buf.MultiBuffer, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.chunks) == 0 {
		return nil, io.EOF
	}
	mb := s.chunks[0]
	s.chunks = s.chunks[1:]
	return mb, nil
}

// delayedBufReader blocks for delay before delegating to inner.
type delayedBufReader struct {
	delay time.Duration
	inner buf.Reader
}

func (d *delayedBufReader) ReadMultiBuffer() (buf.MultiBuffer, error) {
	time.Sleep(d.delay)
	return d.inner.ReadMultiBuffer()
}

// TestQueuedTimeoutReaderTimeoutThenReplay: short timeout then drain must return same bytes as inner.
func TestQueuedTimeoutReaderTimeoutThenReplay(t *testing.T) {
	clientHello := []byte{0x16, 0x03, 0x01, 0x00, 0x05, 0x01, 0x02, 0x03, 0x04, 0x05}
	inner := &sliceBufReader{chunks: []buf.MultiBuffer{buf.MergeBytes(nil, clientHello)}}
	delayed := &delayedBufReader{delay: 40 * time.Millisecond, inner: inner}
	q := &queuedTimeoutBufReader{inner: delayed}

	mb, err := q.ReadMultiBufferTimeout(5 * time.Millisecond)
	if err != buf.ErrReadTimeout {
		t.Fatalf("expected timeout, got err=%v mb=%v", err, mb)
	}
	if mb != nil && !mb.IsEmpty() {
		t.Fatalf("expected empty mb on timeout")
	}

	mb2, err := q.ReadMultiBuffer()
	common.Must(err)
	if int(mb2.Len()) != len(clientHello) {
		t.Fatalf("replay len: want %d got %d", len(clientHello), mb2.Len())
	}
	got := make([]byte, mb2.Len())
	mb2, n := buf.SplitBytes(mb2, got)
	buf.ReleaseMulti(mb2)
	if n != len(clientHello) {
		t.Fatalf("split n=%d", n)
	}
	if string(got) != string(clientHello) {
		t.Fatalf("bytes mismatch")
	}
}

// TestCachedReaderSniffThenHandlerRead: sniff path merges into cache; handler reads identical stream.
func TestCachedReaderSniffThenHandlerRead(t *testing.T) {
	clientHello := make([]byte, 200)
	clientHello[0] = 0x16
	clientHello[1] = 0x03
	clientHello[2] = 0x01
	for i := 3; i < len(clientHello); i++ {
		clientHello[i] = byte(i)
	}
	inner := &sliceBufReader{chunks: []buf.MultiBuffer{buf.MergeBytes(nil, clientHello)}}
	q := &queuedTimeoutBufReader{inner: inner}
	cr := &cachedReader{reader: q, logCtx: context.Background()}

	payload := buf.NewWithSize(4096)
	defer payload.Release()
	if err := cr.Cache(payload, 500*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if payload.IsEmpty() {
		t.Fatal("sniff payload empty")
	}

	mb, err := cr.ReadMultiBuffer()
	common.Must(err)
	if int(mb.Len()) != len(clientHello) {
		t.Fatalf("handler first read len want %d got %d", len(clientHello), mb.Len())
	}
	got := make([]byte, mb.Len())
	mb, n := buf.SplitBytes(mb, got)
	buf.ReleaseMulti(mb)
	if n != len(clientHello) || string(got) != string(clientHello) {
		t.Fatal("handler read mismatch")
	}
}

// TestRouteOnlySniffCacheIntact: full cache is replayed to handler (independent of routeOnly flag on SniffingRequest).
func TestRouteOnlySniffCacheIntact(t *testing.T) {
	data := []byte{0x16, 0x03, 0x01, 0x00, 0x03, 1, 2, 3}
	inner := &sliceBufReader{chunks: []buf.MultiBuffer{buf.MergeBytes(nil, data)}}
	cr := &cachedReader{reader: &queuedTimeoutBufReader{inner: inner}, logCtx: context.Background()}
	payload := buf.NewWithSize(4096)
	defer payload.Release()
	common.Must(cr.Cache(payload, time.Second))
	mb, err := cr.ReadMultiBuffer()
	common.Must(err)
	if int(mb.Len()) != len(data) {
		t.Fatal("cache replay broken")
	}
	buf.ReleaseMulti(mb)
}
