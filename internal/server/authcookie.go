// Copyright 2025
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// authCookieName is the remember-me cookie issued after a successful Basic
// auth when --auth-cookie-ttl is set. It spares browsers from re-prompting
// (Basic credentials are forgotten on browser restart); every request is
// still authenticated, just by cookie instead of the Authorization header.
const authCookieName = "hfd_auth"

// cookieKey derives the signing key from the configured credentials: both
// fields always run through the hash (no early return on length), and a
// password change silently invalidates every outstanding cookie.
func (s *Server) cookieKey() []byte {
	h := sha256.New()
	h.Write([]byte(s.config.AuthUser))
	h.Write([]byte{0})
	h.Write([]byte(s.config.AuthPass))
	return h.Sum(nil)
}

// mintAuthCookie returns "exp.hmac" where hmac = HMAC-SHA256(key, "user:exp").
func (s *Server) mintAuthCookie(ttl time.Duration) string {
	exp := time.Now().Add(ttl).Unix()
	payload := s.config.AuthUser + ":" + strconv.FormatInt(exp, 10)
	mac := hmac.New(sha256.New, s.cookieKey())
	mac.Write([]byte(payload))
	return strconv.FormatInt(exp, 10) + "." + hex.EncodeToString(mac.Sum(nil))
}

// verifyAuthCookie checks format, expiry, user binding and signature in
// constant time.
func (s *Server) verifyAuthCookie(value string) bool {
	expStr, sigHex, ok := strings.Cut(value, ".")
	if !ok {
		return false
	}
	exp, err := strconv.ParseInt(expStr, 10, 64)
	if err != nil || time.Now().Unix() > exp {
		return false
	}
	sig, err := hex.DecodeString(sigHex)
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, s.cookieKey())
	mac.Write([]byte(s.config.AuthUser + ":" + expStr))
	return subtle.ConstantTimeCompare(mac.Sum(nil), sig) == 1
}

// issueAuthCookie sets the HttpOnly remember-me cookie. SameSite=Lax keeps
// cross-site POSTs from carrying it (the server already blocks cross-origin
// requests); Secure is omitted because LAN deployments are plain HTTP —
// behind an HTTPS reverse proxy you would want it, hence a best-effort hint:
// if the request itself arrived over TLS, mark the cookie Secure.
func (s *Server) issueAuthCookie(w http.ResponseWriter, r *http.Request, ttl time.Duration) {
	http.SetCookie(w, &http.Cookie{
		Name:     authCookieName,
		Value:    s.mintAuthCookie(ttl),
		Path:     "/",
		MaxAge:   int(ttl.Seconds()),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   r.TLS != nil,
	})
}
