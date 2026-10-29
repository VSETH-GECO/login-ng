package server

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
)

// newPatchRouter wires up the VLAN switch submit handler with a sqlmock DB.
func newPatchRouter(t *testing.T) (*gin.Engine, sqlmock.Sqlmock, *Server) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	sqlDB, mock, err := sqlmock.New()
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

	// Seed route: sets username in session
	r.GET("/seed", func(ctx *gin.Context) {
		sess := sessions.Default(ctx)
		sess.Set(sessionUserName, "alice")
		_ = sess.Save()
		ctx.String(http.StatusOK, "seeded")
	})

	r.POST("/switch", switchVLANSubmitHandler(s))

	return r, mock, s
}

// seedCookies performs a GET /seed and returns the Set-Cookie header value.
func seedCookies(t *testing.T, r *gin.Engine) string {
	t.Helper()
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/seed", nil))
	return w.Header().Get("Set-Cookie")
}

// postSwitch sends a POST /switch with the given vlan form value and an optional cookie.
func postSwitch(r *gin.Engine, vlan string, cookieHeader string) *httptest.ResponseRecorder {
	form := url.Values{"vlan": {vlan}}
	req, _ := http.NewRequest(http.MethodPost, "/switch", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if cookieHeader != "" {
		req.Header.Set("Cookie", cookieHeader)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// ── switchVLANSubmitHandler — input validation ────────────────────────────────

func TestSwitchVLANSubmit_invalidVLAN(t *testing.T) {
	r, _, _ := newPatchRouter(t)
	cookies := seedCookies(t, r)

	w := postSwitch(r, "notanumber", cookies)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestSwitchVLANSubmit_emptyVLAN(t *testing.T) {
	r, _, _ := newPatchRouter(t)
	cookies := seedCookies(t, r)

	w := postSwitch(r, "", cookies)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestSwitchVLANSubmit_userNotFound(t *testing.T) {
	r, mock, _ := newPatchRouter(t)
	cookies := seedCookies(t, r)

	mock.ExpectQuery("SELECT").WillReturnError(errors.New("no rows"))

	w := postSwitch(r, "527", cookies)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

func TestSwitchVLANSubmit_vlanNotAllowed(t *testing.T) {
	r, mock, _ := newPatchRouter(t)
	cookies := seedCookies(t, r)

	// locateUser returns a result
	userRows := sqlmock.NewRows([]string{"user_ip", "user_mac", "switch_ip"}).
		AddRow("127.0.0.1", "aabbcc", "10.0.0.1")
	mock.ExpectQuery("SELECT").WillReturnRows(userRows)

	// getSwitchVLAN returns primaryVLAN = 527
	vlanRows := sqlmock.NewRows([]string{"vlan"}).AddRow(527)
	mock.ExpectQuery("SELECT primary_vlan").WillReturnRows(vlanRows)

	// getAvailableVLANs returns only VLAN 527 and 490
	availRows := sqlmock.NewRows([]string{"name", "vlan_id"}).
		AddRow("User 27", 527).
		AddRow("Shared 1", 490)
	mock.ExpectQuery("SELECT name, vlan_id").WillReturnRows(availRows)

	// submit VLAN 999 which is not in the allowed list
	w := postSwitch(r, "999", cookies)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

func TestSwitchVLANSubmit_success(t *testing.T) {
	r, mock, _ := newPatchRouter(t)
	cookies := seedCookies(t, r)

	userRows := sqlmock.NewRows([]string{"user_ip", "user_mac", "switch_ip"}).
		AddRow("127.0.0.1", "aabbcc", "10.0.0.1")
	mock.ExpectQuery("SELECT").WillReturnRows(userRows)

	vlanRows := sqlmock.NewRows([]string{"vlan"}).AddRow(527)
	mock.ExpectQuery("SELECT primary_vlan").WillReturnRows(vlanRows)

	availRows := sqlmock.NewRows([]string{"name", "vlan_id"}).
		AddRow("User 27", 527)
	mock.ExpectQuery("SELECT name, vlan_id").WillReturnRows(availRows)

	mock.ExpectExec("INSERT INTO bouncer_jobs").
		WithArgs("aabbcc", 527).
		WillReturnResult(sqlmock.NewResult(1, 1))

	mock.ExpectExec("INSERT INTO login_logs").
		WithArgs("alice", "aabbcc").
		WillReturnResult(sqlmock.NewResult(1, 1))

	w := postSwitch(r, "527", cookies)
	// expect redirect to /switch/success
	if w.Code != http.StatusSeeOther {
		t.Errorf("expected 303 redirect, got %d: %s", w.Code, w.Body.String())
	}
	if loc := w.Header().Get("Location"); loc != "/switch/success" {
		t.Errorf("expected redirect to /switch/success, got %q", loc)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

// ── patch — missing username in session ───────────────────────────────────────

func TestPatch_missingUsernameInSession(t *testing.T) {
	gin.SetMode(gin.TestMode)

	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer sqlDB.Close()

	s := &Server{Log: zerolog.Nop(), DB: db{sqlDB}}

	r := gin.New()
	store := cookie.NewStore([]byte("test-secret"))
	r.Use(sessions.Sessions("auth-session", store))
	r.LoadHTMLGlob("../templates/*.gohtml")

	r.GET("/patch", func(ctx *gin.Context) {
		// bounce job succeeds
		mock.ExpectExec("INSERT INTO bouncer_jobs").
			WithArgs("aabbcc", 527).
			WillReturnResult(sqlmock.NewResult(1, 1))
		// no username in session → patch should return nil (log-only path)
		err := s.patch(ctx, "aabbcc", 527)
		if err != nil {
			ctx.String(http.StatusInternalServerError, err.Error())
			return
		}
		ctx.String(http.StatusOK, "ok")
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/patch", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 (login log is best-effort), got %d: %s", w.Code, w.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

// ── helpers shared by handler chain tests ────────────────────────────────────

// newFullPatchRouter builds a router with all patch-related routes mounted
// directly (no auth middleware) so handler logic can be tested in isolation.
func newFullPatchRouter(t *testing.T) (*gin.Engine, sqlmock.Sqlmock) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })

	s := &Server{Log: zerolog.Nop(), DB: db{sqlDB}}

	r := gin.New()
	store := cookie.NewStore([]byte("test-secret"))
	r.Use(sessions.Sessions("auth-session", store))
	r.LoadHTMLGlob("../templates/*.gohtml")

	// seed username into session
	r.GET("/seed", func(ctx *gin.Context) {
		sess := sessions.Default(ctx)
		sess.Set(sessionUserName, "alice")
		_ = sess.Save()
		ctx.String(http.StatusOK, "ok")
	})

	r.GET("/connect", connectHandler(s))
	r.GET("/disconnect", disconnectHandler(s))
	r.GET("/switch", switchVLANHandler(s))
	r.GET("/switch/success", switchVLANSuccessHandler(s))

	return r, mock
}

// userAndVLANExpects sets up the two DB expectations for locateUser + getSwitchVLAN.
func userAndVLANExpects(mock sqlmock.Sqlmock, mac, switchIP string, vlan int) {
	mock.ExpectQuery("SELECT").WillReturnRows(
		sqlmock.NewRows([]string{"user_ip", "user_mac", "switch_ip"}).
			AddRow("127.0.0.1", mac, switchIP),
	)
	mock.ExpectQuery("SELECT primary_vlan").WillReturnRows(
		sqlmock.NewRows([]string{"vlan"}).AddRow(vlan),
	)
}

// ── connectHandler ────────────────────────────────────────────────────────────

func TestConnectHandler_success(t *testing.T) {
	r, mock := newFullPatchRouter(t)
	cookies := seedCookies(t, r)

	userAndVLANExpects(mock, "aabbcc", "10.0.0.1", 527)
	mock.ExpectExec("INSERT INTO bouncer_jobs").WithArgs("aabbcc", 527).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("INSERT INTO login_logs").WithArgs("alice", "aabbcc").
		WillReturnResult(sqlmock.NewResult(1, 1))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/connect", nil)
	req.Header.Set("Cookie", cookies)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

func TestConnectHandler_userNotFound(t *testing.T) {
	r, mock := newFullPatchRouter(t)
	cookies := seedCookies(t, r)

	mock.ExpectQuery("SELECT").WillReturnError(errors.New("no rows"))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/connect", nil)
	req.Header.Set("Cookie", cookies)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

func TestConnectHandler_switchVLANNotFound(t *testing.T) {
	r, mock := newFullPatchRouter(t)
	cookies := seedCookies(t, r)

	mock.ExpectQuery("SELECT").WillReturnRows(
		sqlmock.NewRows([]string{"user_ip", "user_mac", "switch_ip"}).
			AddRow("127.0.0.1", "aabbcc", "10.0.0.1"),
	)
	mock.ExpectQuery("SELECT primary_vlan").WillReturnError(errors.New("not found"))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/connect", nil)
	req.Header.Set("Cookie", cookies)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

// ── disconnectHandler ─────────────────────────────────────────────────────────

func TestDisconnectHandler_success(t *testing.T) {
	r, mock := newFullPatchRouter(t)
	cookies := seedCookies(t, r)

	mock.ExpectQuery("SELECT").WillReturnRows(
		sqlmock.NewRows([]string{"user_ip", "user_mac", "switch_ip"}).
			AddRow("127.0.0.1", "aabbcc", "10.0.0.1"),
	)
	mock.ExpectExec("INSERT INTO bouncer_jobs").WithArgs("aabbcc", logonVLAN).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("INSERT INTO login_logs").WithArgs("alice", "aabbcc").
		WillReturnResult(sqlmock.NewResult(1, 1))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/disconnect", nil)
	req.Header.Set("Cookie", cookies)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

func TestDisconnectHandler_userNotFound(t *testing.T) {
	r, mock := newFullPatchRouter(t)
	cookies := seedCookies(t, r)

	mock.ExpectQuery("SELECT").WillReturnError(errors.New("no rows"))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/disconnect", nil)
	req.Header.Set("Cookie", cookies)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

// ── switchVLANHandler ─────────────────────────────────────────────────────────

func TestSwitchVLANHandler_success(t *testing.T) {
	r, mock := newFullPatchRouter(t)
	cookies := seedCookies(t, r)

	userAndVLANExpects(mock, "aabbcc", "10.0.0.1", 527)
	mock.ExpectQuery("SELECT name, vlan_id").WillReturnRows(
		sqlmock.NewRows([]string{"name", "vlan_id"}).
			AddRow("User 27", 527).
			AddRow("Shared 1", 490),
	)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/switch", nil)
	req.Header.Set("Cookie", cookies)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

func TestSwitchVLANHandler_userNotFound(t *testing.T) {
	r, mock := newFullPatchRouter(t)
	cookies := seedCookies(t, r)

	mock.ExpectQuery("SELECT").WillReturnError(errors.New("no rows"))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/switch", nil)
	req.Header.Set("Cookie", cookies)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
}

func TestSwitchVLANHandler_switchVLANNotFound(t *testing.T) {
	r, mock := newFullPatchRouter(t)
	cookies := seedCookies(t, r)

	mock.ExpectQuery("SELECT").WillReturnRows(
		sqlmock.NewRows([]string{"user_ip", "user_mac", "switch_ip"}).
			AddRow("127.0.0.1", "aabbcc", "10.0.0.1"),
	)
	mock.ExpectQuery("SELECT primary_vlan").WillReturnError(errors.New("not found"))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/switch", nil)
	req.Header.Set("Cookie", cookies)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
}

func TestSwitchVLANHandler_availableVLANsError(t *testing.T) {
	r, mock := newFullPatchRouter(t)
	cookies := seedCookies(t, r)

	userAndVLANExpects(mock, "aabbcc", "10.0.0.1", 527)
	mock.ExpectQuery("SELECT name, vlan_id").WillReturnError(errors.New("timeout"))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/switch", nil)
	req.Header.Set("Cookie", cookies)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
}

// ── switchVLANSuccessHandler ──────────────────────────────────────────────────

func TestSwitchVLANSuccessHandler(t *testing.T) {
	r, _ := newFullPatchRouter(t)
	cookies := seedCookies(t, r)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/switch/success", nil)
	req.Header.Set("Cookie", cookies)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// ── switchVLANSubmitHandler — getSwitchVLAN error branch ─────────────────────

func TestSwitchVLANSubmit_switchVLANError(t *testing.T) {
	r, mock, _ := newPatchRouter(t)
	cookies := seedCookies(t, r)

	mock.ExpectQuery("SELECT").WillReturnRows(
		sqlmock.NewRows([]string{"user_ip", "user_mac", "switch_ip"}).
			AddRow("127.0.0.1", "aabbcc", "10.0.0.1"),
	)
	mock.ExpectQuery("SELECT primary_vlan").WillReturnError(errors.New("not found"))

	w := postSwitch(r, "527", cookies)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

func TestSwitchVLANSubmit_availableVLANsError(t *testing.T) {
	r, mock, _ := newPatchRouter(t)
	cookies := seedCookies(t, r)

	userAndVLANExpects(mock, "aabbcc", "10.0.0.1", 527)
	mock.ExpectQuery("SELECT name, vlan_id").WillReturnError(errors.New("timeout"))

	w := postSwitch(r, "527", cookies)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

func TestSwitchVLANSubmit_bounceJobError(t *testing.T) {
	r, mock, _ := newPatchRouter(t)
	cookies := seedCookies(t, r)

	userAndVLANExpects(mock, "aabbcc", "10.0.0.1", 527)
	mock.ExpectQuery("SELECT name, vlan_id").WillReturnRows(
		sqlmock.NewRows([]string{"name", "vlan_id"}).AddRow("User 27", 527),
	)
	mock.ExpectExec("INSERT INTO bouncer_jobs").
		WithArgs("aabbcc", 527).
		WillReturnError(errors.New("deadlock"))

	w := postSwitch(r, "527", cookies)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

// ── RequiredCheckedIn ─────────────────────────────────────────────────────────

func newCheckedInRouter(t *testing.T, gecoHandler http.HandlerFunc) *gin.Engine {
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
	r.LoadHTMLGlob("../templates/*.gohtml")

	r.GET("/seed", func(ctx *gin.Context) {
		sess := sessions.Default(ctx)
		sess.Set(sessionUserSub, "user123")
		sess.Set(sessionUserAccessToken, "tok")
		_ = sess.Save()
		ctx.String(http.StatusOK, "ok")
	})
	r.GET("/guarded", RequiredCheckedIn(s), func(ctx *gin.Context) {
		ctx.String(http.StatusOK, "reached")
	})
	return r
}

func TestRequiredCheckedIn_passes(t *testing.T) {
	r := newCheckedInRouter(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	cookies := seedCookiesFromRoute(t, r, "/seed")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/guarded", nil)
	req.Header.Set("Cookie", cookies)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK || w.Body.String() != "reached" {
		t.Errorf("expected handler to be reached, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRequiredCheckedIn_forbidden(t *testing.T) {
	r := newCheckedInRouter(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
	})
	cookies := seedCookiesFromRoute(t, r, "/seed")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/guarded", nil)
	req.Header.Set("Cookie", cookies)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", w.Code)
	}
}

// seedCookiesFromRoute seeds a session by hitting any arbitrary seed URL.
func seedCookiesFromRoute(t *testing.T, r *gin.Engine, path string) string {
	t.Helper()
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
	return w.Header().Get("Set-Cookie")
}
