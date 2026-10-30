package server

import (
	"encoding/gob"
	"net/http"
	"net/netip"
	"time"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
)

// Server is the server struct
type Server struct {
	Log            zerolog.Logger
	DB             db
	OIDCProvider   *OIDCProvider
	GecoAPIConfig  *GecoAPIConfig
	SessionSecret  string
	TrustedProxies []string
	// LoginNetwork is the network users are in before they are connected (logon VLAN).
	LoginNetwork  netip.Prefix
	SecureCookies bool
}

// ListenAndServe sets up the HTTP server and starts listening
func (s *Server) ListenAndServe(listen string) error {
	r := gin.Default()
	if err := r.SetTrustedProxies(s.TrustedProxies); err != nil {
		return err
	}

	// To store custom types in our cookies,
	// we must first register them using gob.Register
	gob.Register(map[string]interface{}{})

	store := cookie.NewStore([]byte(s.SessionSecret))
	store.Options(sessions.Options{
		MaxAge:   int(4 * 24 * time.Hour.Seconds()), // 4 days for the entire LAN duration
		Secure:   s.SecureCookies,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})
	r.Use(sessions.Sessions("auth-session", store))

	r.Static("/static", "static")
	r.LoadHTMLGlob("templates/*.gohtml")

	r.GET("/", indexHandler(s))

	r.GET("/login", LoginHandler(s.OIDCProvider))
	r.GET("/callback", CallbackHandler(s.OIDCProvider, "/connect"))
	r.GET("/connect", RequireAuthenticated, RequiredCheckedIn(s), connectHandler(s))

	r.GET("/logout", LogoutHandler(s.OIDCProvider, "/"))
	r.GET("/disconnect", RequireAuthenticated, RequiredCheckedIn(s), disconnectHandler(s))

	r.GET("/switch", RequireAuthenticated, RequiredCheckedIn(s), switchVLANHandler(s))
	r.POST("/switch", RequireAuthenticated, RequiredCheckedIn(s), switchVLANSubmitHandler(s))
	r.GET("/switch/success", RequireAuthenticated, RequiredCheckedIn(s), switchVLANSuccessHandler(s))

	// polled by the success page while the user is moved to the new VLAN
	r.GET("/status", RequireAuthenticated, statusHandler(s))

	r.GET("/liveness", livenessHandler(s))
	r.GET("/readiness", readinessHandler(s))

	s.Log.Info().Str("addr", listen).Msg("Listening...")
	return http.ListenAndServe(listen, r)
}

func indexHandler(s *Server) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		session := sessions.Default(ctx)
		ctx.HTML(http.StatusOK, "index.gohtml", gin.H{
			"isAuthenticated": session.Get(sessionUserSub) != nil,
			"inLoginNetwork":  s.inLoginNetwork(ctx.ClientIP()),
			"username":        session.Get(sessionUserName),
		})
	}
}

// inLoginNetwork reports whether ip is inside the configured login network.
func (s *Server) inLoginNetwork(ip string) bool {
	return prefixContains(s.LoginNetwork, ip)
}

// prefixContains reports whether ip is inside prefix.
func prefixContains(prefix netip.Prefix, ip string) bool {
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return false
	}
	return prefix.Contains(addr.Unmap())
}

// executed every few seconds in order to check the container status.
func livenessHandler(s *Server) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		if err := s.DB.PingContext(ctx.Request.Context()); err != nil {
			ctx.AbortWithStatus(http.StatusServiceUnavailable)
			return
		}
		ctx.Writer.Write([]byte("ok"))
	}
}

// executed once to verify the successfull startup of your container.
func readinessHandler(s *Server) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		ctx.Writer.Write([]byte("ready"))
	}
}

func renderError(ctx *gin.Context, page string, code int, msg string, retryURL ...string) {
	pageContent := gin.H{
		"error": msg,
	}

	session := sessions.Default(ctx)
	if uname := session.Get(sessionUserName); uname != nil {
		pageContent["username"] = uname
	}

	if len(retryURL) > 0 {
		pageContent["retryURL"] = retryURL[0]
	}

	ctx.HTML(code, page, pageContent)
}
