// mockserver is a development helper that mimics the two external services
// login-ng depends on:
//
//  1. An OIDC provider (subset used by go-oidc + oauth2):
//     GET /.well-known/openid-configuration
//     GET /jwks.json
//     GET /authorize          → redirects to callback with code
//     POST /token             → returns id_token + access_token
//
//  2. The GeCo API:
//     GET /api/v1/lan_parties/{id}/me
//     → 200 (checked-in) by default
//     → set ?status=422 on the endpoint URL to simulate "not checked in"
//
// Usage:
//
//	go run ./mockserver            # listens on :8090
//	go run ./mockserver -listen :9000
package main

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

var listenFlag = flag.String("listen", ":8090", "Address to listen on")

// ---------------------------------------------------------------------------
// RSA key (generated once at startup)
// ---------------------------------------------------------------------------

const keyID = "mockkey1"

var privKey *rsa.PrivateKey

func init() {
	var err error
	privKey, err = rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		log.Fatalf("failed to generate RSA key: %v", err)
	}
}

// ---------------------------------------------------------------------------
// In-memory authorization-code store
// ---------------------------------------------------------------------------

type pendingAuth struct {
	nonce       string
	redirectURI string
	clientID    string
}

var (
	mu    sync.Mutex
	codes = map[string]pendingAuth{}
)

// ---------------------------------------------------------------------------
// main
// ---------------------------------------------------------------------------

func main() {
	flag.Parse()

	mux := http.NewServeMux()

	// OIDC discovery + JWKS
	mux.HandleFunc("/.well-known/openid-configuration", handleDiscovery)
	mux.HandleFunc("/jwks.json", handleJWKS)

	// OAuth2 flows
	mux.HandleFunc("/authorize", handleAuthorize)
	mux.HandleFunc("/token", handleToken)

	// GeCo API
	mux.HandleFunc("/api/v1/lan_parties/", handleGecoMe)

	log.Printf("mock server listening on %s", *listenFlag)
	log.Fatal(http.ListenAndServe(*listenFlag, mux))
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func issuerBase(r *http.Request) string {
	return "http://" + r.Host
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.Encode(v) //nolint:errcheck
}

func randomHex(n int) string {
	b := make([]byte, n)
	rand.Read(b) //nolint:errcheck
	return fmt.Sprintf("%x", b)
}

// ---------------------------------------------------------------------------
// OIDC discovery
// ---------------------------------------------------------------------------

func handleDiscovery(w http.ResponseWriter, r *http.Request) {
	base := issuerBase(r)
	writeJSON(w, map[string]any{
		"issuer":                                base,
		"authorization_endpoint":                base + "/authorize",
		"token_endpoint":                        base + "/token",
		"jwks_uri":                              base + "/jwks.json",
		"response_types_supported":              []string{"code"},
		"subject_types_supported":               []string{"public"},
		"id_token_signing_alg_values_supported": []string{"RS256"},
		"scopes_supported":                      []string{"openid", "user:lan:read"},
		"code_challenge_methods_supported":      []string{"S256"},
	})
}

// ---------------------------------------------------------------------------
// JWKS
// ---------------------------------------------------------------------------

func handleJWKS(w http.ResponseWriter, r *http.Request) {
	pub := privKey.PublicKey
	n := base64.RawURLEncoding.EncodeToString(pub.N.Bytes())
	e := base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes())
	writeJSON(w, map[string]any{
		"keys": []map[string]any{{
			"kty": "RSA",
			"use": "sig",
			"alg": "RS256",
			"kid": keyID,
			"n":   n,
			"e":   e,
		}},
	})
}

// ---------------------------------------------------------------------------
// /authorize  →  redirect to redirect_uri with ?code=…&state=…
// ---------------------------------------------------------------------------

func handleAuthorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	redirectURI := q.Get("redirect_uri")
	state := q.Get("state")
	nonce := q.Get("nonce")

	if redirectURI == "" {
		http.Error(w, "missing redirect_uri", http.StatusBadRequest)
		return
	}

	clientID := q.Get("client_id")

	code := randomHex(16)
	mu.Lock()
	codes[code] = pendingAuth{nonce: nonce, redirectURI: redirectURI, clientID: clientID}
	mu.Unlock()

	target, err := url.Parse(redirectURI)
	if err != nil {
		http.Error(w, "invalid redirect_uri", http.StatusBadRequest)
		return
	}
	qp := target.Query()
	qp.Set("code", code)
	qp.Set("state", state)
	target.RawQuery = qp.Encode()

	http.Redirect(w, r, target.String(), http.StatusFound)
}

// ---------------------------------------------------------------------------
// /token  →  returns id_token + access_token
// ---------------------------------------------------------------------------

func handleToken(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}

	code := r.FormValue("code")
	mu.Lock()
	auth, ok := codes[code]
	if ok {
		delete(codes, code)
	}
	mu.Unlock()

	if !ok {
		http.Error(w, "unknown code", http.StatusBadRequest)
		return
	}

	// client_id may arrive as HTTP Basic Auth (oauth2 library default) or in the form body.
	clientID := auth.clientID
	if clientID == "" {
		if id, _, ok := r.BasicAuth(); ok {
			clientID = id
		}
	}
	if clientID == "" {
		clientID = r.FormValue("client_id")
	}

	now := time.Now()
	claims := map[string]any{
		"iss":      issuerBase(r),
		"sub":      "1607",
		"aud":      []string{clientID},
		"iat":      now.Unix(),
		"exp":      now.Add(time.Hour).Unix(),
		"nonce":    auth.nonce,
		"username": "devuser",
	}

	idToken, err := signRS256JWT(claims)
	if err != nil {
		http.Error(w, "failed to sign id_token", http.StatusInternalServerError)
		return
	}

	writeJSON(w, map[string]any{
		"access_token": "mock-access-token-" + randomHex(8),
		"token_type":   "Bearer",
		"expires_in":   3600,
		"id_token":     idToken,
	})
}

// ---------------------------------------------------------------------------
// GeCo API  GET /api/v1/lan_parties/{id}/me
//
// Returns 200 (checked-in) by default.
// Append ?status=422 to GECO_USERSTATUS_ENDPOINT to simulate no ticket.
// ---------------------------------------------------------------------------

func handleGecoMe(w http.ResponseWriter, r *http.Request) {
	if !strings.HasSuffix(r.URL.Path, "/me") {
		http.NotFound(w, r)
		return
	}

	if r.URL.Query().Get("status") == "422" {
		w.WriteHeader(http.StatusUnprocessableEntity)
		writeJSON(w, map[string]string{"message": "User has no ticket assigned for this LanParty"})
		return
	}

	writeJSON(w, map[string]any{
		"user": map[string]any{"id": 1607, "username": "devuser"},
		"seat": map[string]any{"id": 14, "name": "14"},
	})
}

// ---------------------------------------------------------------------------
// RS256 JWT (stdlib only — no external dependency)
// ---------------------------------------------------------------------------

func signRS256JWT(claims map[string]any) (string, error) {
	header := map[string]string{"alg": "RS256", "typ": "JWT", "kid": keyID}

	headerJSON, err := json.Marshal(header)
	if err != nil {
		return "", err
	}
	claimsJSON, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}

	headerEnc := base64.RawURLEncoding.EncodeToString(headerJSON)
	claimsEnc := base64.RawURLEncoding.EncodeToString(claimsJSON)
	sigInput := headerEnc + "." + claimsEnc

	h := sha256.Sum256([]byte(sigInput))
	sig, err := rsa.SignPKCS1v15(rand.Reader, privKey, crypto.SHA256, h[:])
	if err != nil {
		return "", err
	}

	return sigInput + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}
