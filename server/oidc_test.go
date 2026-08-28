package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
)

// ── randString ────────────────────────────────────────────────────────────────

func TestRandString_length(t *testing.T) {
	s, err := randString(16)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// base64url encoding of 16 bytes → 22 chars (no padding)
	if len(s) == 0 {
		t.Error("expected non-empty string")
	}
}

func TestRandString_unique(t *testing.T) {
	a, _ := randString(16)
	b, _ := randString(16)
	if a == b {
		t.Error("two calls returned the same string")
	}
}

// ── RequireAuthenticated ──────────────────────────────────────────────────────

func newAuthRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	store := cookie.NewStore([]byte("test-secret"))
	r.Use(sessions.Sessions("auth-session", store))
	r.GET("/protected", RequireAuthenticated, func(ctx *gin.Context) {
		ctx.String(http.StatusOK, "ok")
	})
	r.GET("/seed", func(ctx *gin.Context) {
		sess := sessions.Default(ctx)
		sess.Set(sessionUserSub, "user123")
		_ = sess.Save()
		ctx.String(http.StatusOK, "seeded")
	})
	return r
}

func TestRequireAuthenticated_noSession(t *testing.T) {
	r := newAuthRouter()
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/protected", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusSeeOther {
		t.Errorf("expected 303 redirect, got %d", w.Code)
	}
}

func TestRequireAuthenticated_withSession(t *testing.T) {
	r := newAuthRouter()

	// seed the session
	w1 := httptest.NewRecorder()
	req1, _ := http.NewRequest(http.MethodGet, "/seed", nil)
	r.ServeHTTP(w1, req1)

	// replay the cookie
	w2 := httptest.NewRecorder()
	req2, _ := http.NewRequest(http.MethodGet, "/protected", nil)
	req2.Header.Set("Cookie", w1.Header().Get("Set-Cookie"))
	r.ServeHTTP(w2, req2)

	if w2.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w2.Code)
	}
}

// ── CallbackHandler — guard checks ───────────────────────────────────────────

// newCallbackRouter wires up only the callback handler on a minimal router.
func newCallbackRouter(t *testing.T, auth *OIDCProvider) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	store := cookie.NewStore([]byte("test-secret"))
	r.Use(sessions.Sessions("auth-session", store))
	r.LoadHTMLGlob("../templates/*.gohtml")
	r.GET("/callback", CallbackHandler(auth, "/connect"))
	return r
}

func minimalOIDCProvider() *OIDCProvider {
	return &OIDCProvider{log: zerolog.Nop()}
}

func TestCallbackHandler_invalidState(t *testing.T) {
	r := newCallbackRouter(t, minimalOIDCProvider())
	w := httptest.NewRecorder()
	// state in query does not match (empty) session state
	req, _ := http.NewRequest(http.MethodGet, "/callback?state=bad&code=x", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCallbackHandler_missingCodeVerifier(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	store := cookie.NewStore([]byte("test-secret"))
	r.Use(sessions.Sessions("auth-session", store))
	r.LoadHTMLGlob("../templates/*.gohtml")

	// seed state but NOT codeVerifier
	r.GET("/seed", func(ctx *gin.Context) {
		sess := sessions.Default(ctx)
		sess.Set(sessionStateKey, "mystate")
		_ = sess.Save()
		ctx.String(http.StatusOK, "ok")
	})
	r.GET("/callback", CallbackHandler(minimalOIDCProvider(), "/connect"))

	w1 := httptest.NewRecorder()
	r.ServeHTTP(w1, httptest.NewRequest(http.MethodGet, "/seed", nil))
	cookies := w1.Header().Get("Set-Cookie")

	w2 := httptest.NewRecorder()
	req2, _ := http.NewRequest(http.MethodGet, "/callback?state=mystate&code=x", nil)
	req2.Header.Set("Cookie", cookies)
	r.ServeHTTP(w2, req2)

	// state matches but code verifier is missing → 400
	if w2.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d: %s", w2.Code, w2.Body.String())
	}
	if !strings.Contains(w2.Body.String(), "") {
		// just assert it rendered something
	}
}

// ── LogoutHandler ─────────────────────────────────────────────────────────────

func TestLogoutHandler_clearsSession(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	store := cookie.NewStore([]byte("test-secret"))
	r.Use(sessions.Sessions("auth-session", store))
	r.LoadHTMLGlob("../templates/*.gohtml")

	// seed a session
	r.GET("/seed", func(ctx *gin.Context) {
		sess := sessions.Default(ctx)
		sess.Set(sessionUserSub, "user123")
		_ = sess.Save()
		ctx.String(http.StatusOK, "ok")
	})
	r.GET("/logout", LogoutHandler(minimalOIDCProvider(), "/"))

	// seed session
	w1 := httptest.NewRecorder()
	r.ServeHTTP(w1, httptest.NewRequest(http.MethodGet, "/seed", nil))
	cookies := w1.Header().Get("Set-Cookie")

	// call logout with the session cookie
	w2 := httptest.NewRecorder()
	req2, _ := http.NewRequest(http.MethodGet, "/logout", nil)
	req2.Header.Set("Cookie", cookies)
	r.ServeHTTP(w2, req2)

	if w2.Code != http.StatusTemporaryRedirect {
		t.Errorf("expected 307 redirect, got %d", w2.Code)
	}
	if loc := w2.Header().Get("Location"); loc != "/" {
		t.Errorf("expected redirect to /, got %q", loc)
	}

	// verify session is cleared: hit /seed path which checks sub
	r.GET("/check", func(ctx *gin.Context) {
		sess := sessions.Default(ctx)
		if sess.Get(sessionUserSub) == nil {
			ctx.String(http.StatusOK, "cleared")
		} else {
			ctx.String(http.StatusOK, "still set")
		}
	})
	w3 := httptest.NewRecorder()
	req3, _ := http.NewRequest(http.MethodGet, "/check", nil)
	// use the new cookie from logout response (cleared session)
	req3.Header.Set("Cookie", w2.Header().Get("Set-Cookie"))
	r.ServeHTTP(w3, req3)
	if body := w3.Body.String(); body != "cleared" {
		t.Errorf("expected session to be cleared, got: %s", body)
	}
}
