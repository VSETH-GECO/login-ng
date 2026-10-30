package server

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
)

// newMinimalRouter builds a Gin router with just the health + index routes,
// backed by a sqlmock DB with ping monitoring enabled.
func newMinimalRouter(t *testing.T) (*gin.Engine, sqlmock.Sqlmock) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	sqlDB, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(true))
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })

	s := &Server{
		Log:           zerolog.Nop(),
		DB:            db{sqlDB},
		SessionSecret: "test-secret",
		LoginNetwork:  netip.MustParsePrefix("10.233.64.0/20"),
	}

	r := gin.New()
	store := cookie.NewStore([]byte(s.SessionSecret))
	r.Use(sessions.Sessions("auth-session", store))
	r.LoadHTMLGlob("../templates/*.gohtml")
	r.GET("/liveness", livenessHandler(s))
	r.GET("/readiness", readinessHandler(s))
	r.GET("/", indexHandler(s))

	return r, mock
}

// ── livenessHandler ───────────────────────────────────────────────────────────

func TestLivenessHandler_dbOK(t *testing.T) {
	r, mock := newMinimalRouter(t)
	mock.ExpectPing().WillReturnError(nil)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/liveness", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	if body := w.Body.String(); body != "ok" {
		t.Errorf("expected body 'ok', got %q", body)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

func TestLivenessHandler_dbDown(t *testing.T) {
	r, mock := newMinimalRouter(t)
	mock.ExpectPing().WillReturnError(errDBDown)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/liveness", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", w.Code)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

// sentinel error used in tests
var errDBDown = &dbDownError{}

type dbDownError struct{}

func (e *dbDownError) Error() string { return "db down" }

// ── readinessHandler ──────────────────────────────────────────────────────────

func TestReadinessHandler(t *testing.T) {
	r, _ := newMinimalRouter(t)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/readiness", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	if body := w.Body.String(); body != "ready" {
		t.Errorf("expected body 'ready', got %q", body)
	}
}

// ── indexHandler ──────────────────────────────────────────────────────────────

func TestIndexHandler_unauthenticated(t *testing.T) {
	r, _ := newMinimalRouter(t)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestInLoginNetwork(t *testing.T) {
	s := &Server{LoginNetwork: netip.MustParsePrefix("10.233.64.0/20")}
	for ip, want := range map[string]bool{
		"10.233.64.10":        true,
		"10.233.79.250":       true,
		"10.233.80.1":         false,
		"10.233.6.10":         false,
		"129.132.119.51":      false,
		"::ffff:10.233.64.10": true,
		"not-an-ip":           false,
	} {
		if got := s.inLoginNetwork(ip); got != want {
			t.Errorf("inLoginNetwork(%q) = %v, want %v", ip, got, want)
		}
	}

	// unset network never matches
	if (&Server{}).inLoginNetwork("10.233.64.10") {
		t.Error("expected false when no login network is configured")
	}
}

// authenticatedIndexBody serves "/" for a logged-in user coming from remoteAddr.
func authenticatedIndexBody(t *testing.T, remoteAddr string) string {
	t.Helper()
	r, _ := newMinimalRouter(t)

	seedRouter := gin.New()
	seedRouter.Use(sessions.Sessions("auth-session", cookie.NewStore([]byte("test-secret"))))
	seedRouter.GET("/seed", func(ctx *gin.Context) {
		sess := sessions.Default(ctx)
		sess.Set(sessionUserSub, "user123")
		_ = sess.Save()
	})
	w1 := httptest.NewRecorder()
	seedRouter.ServeHTTP(w1, httptest.NewRequest(http.MethodGet, "/seed", nil))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = remoteAddr
	req.Header.Set("Cookie", w1.Header().Get("Set-Cookie"))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w.Body.String()
}

func TestIndexHandler_loginNetworkShowsConnectOnly(t *testing.T) {
	body := authenticatedIndexBody(t, "10.233.64.10:1234")
	if !strings.Contains(body, "Connect") || strings.Contains(body, "Disconnect") {
		t.Errorf("expected only Connect in login network, got: %s", body)
	}
}

func TestIndexHandler_outsideLoginNetworkShowsDisconnect(t *testing.T) {
	body := authenticatedIndexBody(t, "10.233.27.10:1234")
	if !strings.Contains(body, "Disconnect") || strings.Contains(body, ">Connect<") {
		t.Errorf("expected Disconnect outside login network, got: %s", body)
	}
}

func TestIndexHandler_authenticated(t *testing.T) {
	r, _ := newMinimalRouter(t)

	// seed session
	seedRouter := gin.New()
	store := cookie.NewStore([]byte("test-secret"))
	seedRouter.Use(sessions.Sessions("auth-session", store))
	seedRouter.GET("/seed", func(ctx *gin.Context) {
		sess := sessions.Default(ctx)
		sess.Set(sessionUserSub, "user123")
		sess.Set(sessionUserName, "alice")
		_ = sess.Save()
		ctx.String(http.StatusOK, "seeded")
	})
	w1 := httptest.NewRecorder()
	seedRouter.ServeHTTP(w1, httptest.NewRequest(http.MethodGet, "/seed", nil))
	cookies := w1.Header().Get("Set-Cookie")

	w2 := httptest.NewRecorder()
	req2, _ := http.NewRequest(http.MethodGet, "/", nil)
	req2.Header.Set("Cookie", cookies)
	r.ServeHTTP(w2, req2)

	if w2.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w2.Code)
	}
	if !strings.Contains(w2.Body.String(), "alice") {
		t.Errorf("expected username 'alice' in response body, got: %s", w2.Body.String())
	}
}
