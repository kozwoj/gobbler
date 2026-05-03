package gobblerclient

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// ok200 returns a test server that always replies 200 with an empty ingest result.
func ok200(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"ingested":0,"rejected":null}`))
	}))
}

func TestRealClient_New_ReturnsClient(t *testing.T) {
	srv := ok200(t)
	defer srv.Close()
	c, err := New(srv.URL, WithTypes("alpha"))
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	if c == nil {
		t.Fatal("New() returned nil client")
	}
}

func TestRealClient_Log_UnknownType_ReturnsError(t *testing.T) {
	c, _ := New("http://localhost:8080", WithTypes("alpha"), WithBatchSize(100))
	err := c.Log("unknown", map[string]any{"x": 1})
	if err == nil {
		t.Fatal("Log() with unknown type returned nil, want error")
	}
}

func TestRealClient_Log_UnknownType_NoRegisteredTypes(t *testing.T) {
	c, _ := New("http://localhost:8080", WithBatchSize(100)) // no types registered
	err := c.Log("alpha", map[string]any{"x": 1})
	if err == nil {
		t.Fatal("Log() with no registered types returned nil, want error")
	}
}

func TestRealClient_Log_KnownType_BufferGrows(t *testing.T) {
	srv := ok200(t)
	defer srv.Close()
	c, _ := New(srv.URL, WithTypes("alpha"), WithBatchSize(10))
	rc := c.(*realClient) // assert that it's a *realClient to access the buffer

	for i := 0; i < 5; i++ {
		if err := c.Log("alpha", map[string]any{"i": i}); err != nil {
			t.Fatalf("Log() error at i=%d: %v", i, err)
		}
	}

	rc.mu.Lock()
	n := len(rc.buf)
	rc.mu.Unlock()

	if n != 5 {
		t.Errorf("buffer len = %d, want 5", n)
	}
}

func TestRealClient_Log_ThresholdTrigger_DrainBuffer(t *testing.T) {
	srv := ok200(t)
	defer srv.Close()
	const batchSize = 3
	c, _ := New(srv.URL, WithTypes("alpha"), WithBatchSize(batchSize))
	rc := c.(*realClient)

	// Log batchSize-1 items: buffer should hold them all.
	for i := 0; i < batchSize-1; i++ {
		if err := c.Log("alpha", map[string]any{"i": i}); err != nil {
			t.Fatalf("Log() error at i=%d: %v", i, err)
		}
	}

	rc.mu.Lock()
	before := len(rc.buf)
	rc.mu.Unlock()

	if before != batchSize-1 {
		t.Errorf("buffer before threshold = %d, want %d", before, batchSize-1)
	}

	// The batchSize-th item triggers flush (stub drains the buffer).
	if err := c.Log("alpha", map[string]any{"i": batchSize - 1}); err != nil {
		t.Fatalf("Log() at threshold returned error: %v", err)
	}

	rc.mu.Lock()
	after := len(rc.buf)
	rc.mu.Unlock()

	if after != 0 {
		t.Errorf("buffer after threshold flush = %d, want 0", after)
	}
}

func TestRealClient_Log_MultipleTypes(t *testing.T) {
	c, _ := New("http://localhost:8080", WithTypes("alpha", "beta"), WithBatchSize(100)) // no flush triggered

	if err := c.Log("alpha", map[string]any{"a": 1}); err != nil {
		t.Errorf("Log(alpha) error: %v", err)
	}
	if err := c.Log("beta", map[string]any{"b": 2}); err != nil {
		t.Errorf("Log(beta) error: %v", err)
	}
	if err := c.Log("gamma", map[string]any{"g": 3}); err == nil {
		t.Error("Log(gamma) returned nil, want error for unregistered type")
	}
}

func TestRealClient_Flush_DrainBuffer(t *testing.T) {
	srv := ok200(t)
	defer srv.Close()
	c, _ := New(srv.URL, WithTypes("alpha"), WithBatchSize(10))
	rc := c.(*realClient)

	for i := 0; i < 4; i++ {
		_ = c.Log("alpha", map[string]any{"i": i})
	}

	if err := c.Flush(); err != nil {
		t.Fatalf("Flush() error: %v", err)
	}

	rc.mu.Lock()
	n := len(rc.buf)
	rc.mu.Unlock()

	if n != 0 {
		t.Errorf("buffer after Flush() = %d, want 0", n)
	}
}

func TestRealClient_Close_DrainBuffer(t *testing.T) {
	srv := ok200(t)
	defer srv.Close()
	c, _ := New(srv.URL, WithTypes("alpha"), WithBatchSize(10))
	rc := c.(*realClient)

	_ = c.Log("alpha", map[string]any{"x": 1})

	if err := c.Close(); err != nil {
		t.Fatalf("Close() error: %v", err)
	}

	rc.mu.Lock()
	n := len(rc.buf)
	rc.mu.Unlock()

	if n != 0 {
		t.Errorf("buffer after Close() = %d, want 0", n)
	}
}

func TestRealClient_Close_Idempotent(t *testing.T) {
	srv := ok200(t)
	defer srv.Close()
	c, _ := New(srv.URL, WithTypes("alpha"))

	if err := c.Close(); err != nil {
		t.Fatalf("first Close() error: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("second Close() error: %v", err)
	}
}

func TestRealClient_Close_ForceDrainOn5xx(t *testing.T) {
	srv := newFakeIngest(t, 500, `{"error":"server error"}`, nil)
	defer srv.Close()

	c, _ := New(srv.URL, WithTypes("alpha"), WithBatchSize(100), WithMaxFlushRetries(10), WithFlushInterval(time.Hour))
	rc := c.(*realClient)

	_ = c.Log("alpha", map[string]any{"x": 1})
	err := c.Close()

	if err == nil {
		t.Fatal("Close() with 5xx server returned nil, want error")
	}

	rc.mu.Lock()
	n := len(rc.buf)
	rc.mu.Unlock()
	if n != 0 {
		t.Errorf("buffer after Close() with 5xx = %d, want 0 (force drained)", n)
	}
}

func TestRealClient_Ticker_FlushesAfterInterval(t *testing.T) {
	flushed := make(chan struct{}, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case flushed <- struct{}{}:
		default:
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"ingested":1,"rejected":null}`))
	}))
	defer srv.Close()

	c, _ := New(srv.URL, WithTypes("alpha"), WithBatchSize(100), WithFlushInterval(50*time.Millisecond))
	defer c.Close()

	_ = c.Log("alpha", map[string]any{"x": 1})

	select {
	case <-flushed:
		// ticker fired and flushed
	case <-time.After(2 * time.Second):
		t.Fatal("ticker did not flush within 2s")
	}
}
