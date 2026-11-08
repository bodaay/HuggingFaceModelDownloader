package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func newAuthTestServer(ttl time.Duration) *Server {
	s := &Server{}
	s.config.AuthUser = "alice"
	s.config.AuthPass = "s3cret"
	s.config.AuthCookieTTL = ttl
	return s
}

func TestAuthCookieRoundTrip(t *testing.T) {
	s := newAuthTestServer(24 * time.Hour)
	handler := s.basicAuthMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Wrong credentials: 401, no cookie.
	req := httptest.NewRequest("GET", "/api/jobs", nil)
	req.SetBasicAuth("alice", "wrong")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong password: got %d, want 401", rec.Code)
	}
	if len(rec.Result().Cookies()) != 0 {
		t.Fatal("wrong password must not issue a cookie")
	}

	// Correct credentials: 200 + Set-Cookie.
	req = httptest.NewRequest("GET", "/api/jobs", nil)
	req.SetBasicAuth("alice", "s3cret")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("basic auth: got %d, want 200", rec.Code)
	}
	var auth *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == authCookieName {
			auth = c
		}
	}
	if auth == nil {
		t.Fatal("successful basic auth must issue the remember-me cookie")
	}
	if !auth.HttpOnly {
		t.Error("cookie must be HttpOnly")
	}

	// Cookie alone (no Authorization header): 200.
	req = httptest.NewRequest("GET", "/api/jobs", nil)
	req.AddCookie(auth)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("cookie auth: got %d, want 200", rec.Code)
	}

	// Tampered cookie: 401.
	req = httptest.NewRequest("GET", "/api/jobs", nil)
	bad := *auth
	bad.Value = flipByte(bad.Value)
	req.AddCookie(&bad)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("tampered cookie: got %d, want 401", rec.Code)
	}
}

func TestAuthCookieExpiryAndRotation(t *testing.T) {
	// Expired cookie is rejected.
	s := newAuthTestServer(-time.Minute) // TTL in the past => minted already expired
	value := s.mintAuthCookie(-time.Minute)
	if s.verifyAuthCookie(value) {
		t.Fatal("expired cookie must not verify")
	}

	// Credential rotation invalidates outstanding cookies.
	valid := s.mintAuthCookie(time.Hour)
	if !s.verifyAuthCookie(valid) {
		t.Fatal("fresh cookie must verify")
	}
	s.config.AuthPass = "rotated"
	if s.verifyAuthCookie(valid) {
		t.Fatal("cookie must not survive a password change")
	}
}

func TestAuthCookieDisabledByDefault(t *testing.T) {
	s := newAuthTestServer(0)
	handler := s.basicAuthMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest("GET", "/api/jobs", nil)
	req.SetBasicAuth("alice", "s3cret")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if len(rec.Result().Cookies()) != 0 {
		t.Fatal("TTL 0 must not set any cookie (default behavior unchanged)")
	}
}

func flipByte(s string) string {
	b := []byte(s)
	if b[len(b)-1] == 'a' {
		b[len(b)-1] = 'b'
	} else {
		b[len(b)-1] = 'a'
	}
	return string(b)
}
