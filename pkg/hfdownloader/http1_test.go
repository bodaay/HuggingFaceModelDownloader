package hfdownloader

import (
	"net/http"
	"testing"
)

// force-http1 must pin ALPN to http/1.1 so parallel range requests get
// separate TCP connections instead of multiplexing over one HTTP/2
// connection (issue #98), and the default path must keep HTTP/2.
func TestBuildHTTPClientOptsForceHTTP1(t *testing.T) {
	c, err := BuildHTTPClientOpts(nil, true)
	if err != nil {
		t.Fatalf("BuildHTTPClientOpts: %v", err)
	}
	tr, ok := c.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport is %T, want *http.Transport", c.Transport)
	}
	if tr.ForceAttemptHTTP2 {
		t.Error("ForceAttemptHTTP2 must be false when forceHTTP1 is set")
	}
	if tr.HTTP2 != nil {
		t.Error("HTTP2 config must be cleared when forceHTTP1 is set")
	}
	if tr.TLSClientConfig == nil || len(tr.TLSClientConfig.NextProtos) != 1 ||
		tr.TLSClientConfig.NextProtos[0] != "http/1.1" {
		t.Errorf("NextProtos must be pinned to [http/1.1], got %+v", tr.TLSClientConfig)
	}
}

func TestBuildHTTPClientKeepsHTTP2(t *testing.T) {
	c, err := BuildHTTPClient(nil)
	if err != nil {
		t.Fatalf("BuildHTTPClient: %v", err)
	}
	tr, ok := c.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport is %T, want *http.Transport", c.Transport)
	}
	if !tr.ForceAttemptHTTP2 {
		t.Error("default client must keep ForceAttemptHTTP2 (HTTP/2 as before)")
	}
	if tr.TLSClientConfig != nil && len(tr.TLSClientConfig.NextProtos) != 0 {
		t.Errorf("default client must not pin ALPN, got %+v", tr.TLSClientConfig.NextProtos)
	}
}

// InsecureSkipVerify and the ALPN pin must coexist: neither may clobber the
// other when both are requested.
func TestMergeTLSKeepsBothOptions(t *testing.T) {
	tr := newBaseTransport(true)
	mergeTLS(tr, true, true)
	if tr.TLSClientConfig == nil {
		t.Fatal("TLSClientConfig must be set")
	}
	if !tr.TLSClientConfig.InsecureSkipVerify {
		t.Error("InsecureSkipVerify was lost")
	}
	if len(tr.TLSClientConfig.NextProtos) != 1 || tr.TLSClientConfig.NextProtos[0] != "http/1.1" {
		t.Errorf("ALPN pin was lost: %+v", tr.TLSClientConfig.NextProtos)
	}
}
