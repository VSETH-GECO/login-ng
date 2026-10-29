package server

import (
	"encoding/gob"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
)

func init() {
	gob.Register(map[string]interface{}{})
}

// newGecoTestRouter builds a Gin router with a seeded session for userIsCheckedIn tests.
func newGecoTestRouter(t *testing.T, sub, token string, gecoHandler http.HandlerFunc) (*gin.Engine, *Server) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	gecoSrv := httptest.NewServer(gecoHandler)
	t.Cleanup(gecoSrv.Close)

	s := &Server{
		Log: zerolog.Nop(),
		GecoAPIConfig: &GecoAPIConfig{
			LanID:                 "1",
			UserstatusEndpointFmt: gecoSrv.URL + "/api/v1/lan_parties/%s/me",
		},
	}

	r := gin.New()
	store := cookie.NewStore([]byte("test-secret"))
	r.Use(sessions.Sessions("auth-session", store))

	r.GET("/test", func(ctx *gin.Context) {
		sess := sessions.Default(ctx)
		sess.Set(sessionUserSub, sub)
		sess.Set(sessionUserAccessToken, token)
		_ = sess.Save()
		err := s.userIsCheckedIn(ctx)
		if err != nil {
			ctx.String(http.StatusForbidden, err.Error())
			return
		}
		ctx.String(http.StatusOK, "ok")
	})
	return r, s
}

func TestUserIsCheckedIn_ok(t *testing.T) {
	r, _ := newGecoTestRouter(t, "user123", "tok-abc", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/test", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUserIsCheckedIn_notCheckedIn(t *testing.T) {
	r, _ := newGecoTestRouter(t, "user123", "tok-abc", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/test", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", w.Code)
	}
}

func TestUserIsCheckedIn_apiError(t *testing.T) {
	r, _ := newGecoTestRouter(t, "user123", "tok-abc", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/test", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", w.Code)
	}
}

func TestUserIsCheckedIn_missingSub(t *testing.T) {
	gin.SetMode(gin.TestMode)
	s := &Server{
		Log: zerolog.Nop(),
		GecoAPIConfig: &GecoAPIConfig{
			LanID:                 "1",
			UserstatusEndpointFmt: "http://unused/%s/me",
		},
	}

	r := gin.New()
	store := cookie.NewStore([]byte("test-secret"))
	r.Use(sessions.Sessions("auth-session", store))
	r.GET("/test", func(ctx *gin.Context) {
		// intentionally no session keys set
		err := s.userIsCheckedIn(ctx)
		if err != nil {
			ctx.String(http.StatusForbidden, err.Error())
			return
		}
		ctx.String(http.StatusOK, "ok")
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/test", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", w.Code)
	}
}

func TestUserIsCheckedIn_missingToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	s := &Server{
		Log: zerolog.Nop(),
		GecoAPIConfig: &GecoAPIConfig{
			LanID:                 "1",
			UserstatusEndpointFmt: "http://unused/%s/me",
		},
	}

	r := gin.New()
	store := cookie.NewStore([]byte("test-secret"))
	r.Use(sessions.Sessions("auth-session", store))
	r.GET("/test", func(ctx *gin.Context) {
		sess := sessions.Default(ctx)
		// sub is set but access token is not
		sess.Set(sessionUserSub, "user123")
		_ = sess.Save()
		err := s.userIsCheckedIn(ctx)
		if err != nil {
			ctx.String(http.StatusForbidden, err.Error())
			return
		}
		ctx.String(http.StatusOK, "ok")
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/test", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403 for missing token, got %d", w.Code)
	}
}
