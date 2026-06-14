package service

import (
	"net/http"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
)

// relayDialer / applyRelayConnTimeouts wire RELAY_DIAL_TIMEOUT and
// RELAY_TLS_HANDSHAKE_TIMEOUT onto relay transports. These guard the
// "0 = disabled (legacy)" branch so a future change can't silently re-enable
// or drop the connection-establishment bound. See common.RelayDialTimeout doc.
func TestRelayDialer_HonorsTimeout(t *testing.T) {
	orig := common.RelayDialTimeout
	defer func() { common.RelayDialTimeout = orig }()

	common.RelayDialTimeout = 7
	if got := relayDialer().Timeout; got != 7*time.Second {
		t.Errorf("dial timeout = %v, want 7s", got)
	}

	common.RelayDialTimeout = 0 // disabled → no timeout (legacy behavior)
	if got := relayDialer().Timeout; got != 0 {
		t.Errorf("dial timeout with 0 = %v, want 0 (disabled)", got)
	}
}

func TestApplyRelayConnTimeouts(t *testing.T) {
	origDial, origTLS := common.RelayDialTimeout, common.RelayTLSHandshakeTimeout
	defer func() {
		common.RelayDialTimeout = origDial
		common.RelayTLSHandshakeTimeout = origTLS
	}()

	common.RelayDialTimeout = 5
	common.RelayTLSHandshakeTimeout = 8
	tr := &http.Transport{}
	applyRelayConnTimeouts(tr)
	if tr.DialContext == nil {
		t.Error("DialContext not set")
	}
	if tr.TLSHandshakeTimeout != 8*time.Second {
		t.Errorf("TLSHandshakeTimeout = %v, want 8s", tr.TLSHandshakeTimeout)
	}

	// Both disabled → transport left untouched (truly legacy: no custom DialContext).
	common.RelayDialTimeout = 0
	common.RelayTLSHandshakeTimeout = 0
	tr2 := &http.Transport{}
	applyRelayConnTimeouts(tr2)
	if tr2.DialContext != nil {
		t.Error("DialContext set when dial timeout disabled, want nil (legacy)")
	}
	if tr2.TLSHandshakeTimeout != 0 {
		t.Errorf("TLSHandshakeTimeout with 0 = %v, want 0 (disabled)", tr2.TLSHandshakeTimeout)
	}
}

// Constructor-level: the actual relay clients must carry the timeouts, not just
// the helper. Covers the direct client (InitHttpClient) and the http-proxy
// client (NewProxyHttpClient).
func TestRelayClients_WireConnTimeouts(t *testing.T) {
	origDial, origTLS := common.RelayDialTimeout, common.RelayTLSHandshakeTimeout
	defer func() {
		common.RelayDialTimeout = origDial
		common.RelayTLSHandshakeTimeout = origTLS
		InitHttpClient() // restore the package singleton to env-derived state
	}()

	common.RelayDialTimeout = 6
	common.RelayTLSHandshakeTimeout = 9

	InitHttpClient()
	dt, ok := GetHttpClient().Transport.(*http.Transport)
	if !ok {
		t.Fatal("direct client transport is not *http.Transport")
	}
	if dt.DialContext == nil {
		t.Error("direct client: DialContext not wired")
	}
	if dt.TLSHandshakeTimeout != 9*time.Second {
		t.Errorf("direct client: TLSHandshakeTimeout = %v, want 9s", dt.TLSHandshakeTimeout)
	}

	ResetProxyClientCache()
	pc, err := NewProxyHttpClient("http://127.0.0.1:1080")
	if err != nil {
		t.Fatalf("NewProxyHttpClient: %v", err)
	}
	pt, ok := pc.Transport.(*http.Transport)
	if !ok {
		t.Fatal("proxy client transport is not *http.Transport")
	}
	if pt.DialContext == nil {
		t.Error("http-proxy client: DialContext not wired")
	}
	if pt.TLSHandshakeTimeout != 9*time.Second {
		t.Errorf("http-proxy client: TLSHandshakeTimeout = %v, want 9s", pt.TLSHandshakeTimeout)
	}
}
