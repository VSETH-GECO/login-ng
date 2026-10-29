package server

import (
	"net/http"
	"net/http/httptest"
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
	}

	r := gin.New()
	store := cookie.NewStore([]byte(s.SessionSecret))
	r.Use(sessions.Sessions("auth-session", store))
	r.LoadHTMLGlob("../templates/*.gohtml")
	r.GET("/liveness", livenessHandler(s))
	r.GET("/readiness", readinessHandler(s))
	r.GET("/", indexHandler())

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
