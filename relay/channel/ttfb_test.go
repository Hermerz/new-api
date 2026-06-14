package channel

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	common2 "github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relay/common"
)

// A streaming request to an upstream that stalls before returning headers must
// be aborted at RelayTTFBTimeout so the failover loop can try the next channel.
func TestDoRequestWithTTFB_TimesOutOnHeaderStall(t *testing.T) {
	orig := common2.RelayTTFBTimeout
	defer func() { common2.RelayTTFBTimeout = orig }()
	common2.RelayTTFBTimeout = 1

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(3 * time.Second): // stall well past the TTFB budget
		case <-r.Context().Done():
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	info := &common.RelayInfo{IsStream: true}

	start := time.Now()
	resp, err := doRequestWithTTFB(srv.Client(), req, info)
	if err == nil {
		if resp != nil {
			resp.Body.Close()
		}
		t.Fatal("expected TTFB timeout error, got nil")
	}
	if elapsed := time.Since(start); elapsed > 2500*time.Millisecond {
		t.Errorf("timeout fired after %v, want ~1s", elapsed)
	}
}

// A fast streaming response passes through and its body stays fully readable
// (the watchdog is disarmed; cancel-on-close fires only at Body.Close).
func TestDoRequestWithTTFB_PassesFastResponse(t *testing.T) {
	orig := common2.RelayTTFBTimeout
	defer func() { common2.RelayTTFBTimeout = orig }()
	common2.RelayTTFBTimeout = 5

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, "hello")
	}))
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	info := &common.RelayInfo{IsStream: true}

	resp, err := doRequestWithTTFB(srv.Client(), req, info)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "hello" {
		t.Errorf("body = %q, want %q", string(body), "hello")
	}
	if err := resp.Body.Close(); err != nil { // triggers cancel-on-close
		t.Errorf("body close error: %v", err)
	}
}

// Non-streaming requests are never TTFB-bounded — headers legitimately arrive
// only after the full (possibly long) completion.
func TestDoRequestWithTTFB_DisabledForNonStream(t *testing.T) {
	orig := common2.RelayTTFBTimeout
	defer func() { common2.RelayTTFBTimeout = orig }()
	common2.RelayTTFBTimeout = 1

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(1500 * time.Millisecond) // longer than TTFB, but must NOT be cut off
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, "ok")
	}))
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	info := &common.RelayInfo{IsStream: false}

	resp, err := doRequestWithTTFB(srv.Client(), req, info)
	if err != nil {
		t.Fatalf("non-stream request should not be TTFB-bounded, got: %v", err)
	}
	resp.Body.Close()
}
