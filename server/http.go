package server

import (
	"encoding/gob"
	"net/http"
	"time"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
)

// Server is the server struct
type Server struct {
	Log           zerolog.Logger
	DB            db
	OIDCProvider  *OIDCProvider
	GecoAPIConfig *GecoAPIConfig
	SessionSecret string
}

// ListenAndServe sets up the HTTP server and starts listening
func (s *Server) ListenAndServe(listen string) error {
	r := gin.Default()

	// To store custom types in our cookies,
	// we must first register them using gob.Register
	gob.Register(map[string]interface{}{})

	store := cookie.NewStore([]byte(s.SessionSecret))
	store.Options(sessions.Options{
		MaxAge:   int(4 * 24 * time.Hour.Seconds()), // 4 days for the entire LAN duration
		Secure:   false,                             // localhost
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})
	r.Use(sessions.Sessions("auth-session", store))

	r.Static("/static", "static")
	r.LoadHTMLGlob("templates/*.gohtml")

	r.GET("/", func(ctx *gin.Context) {
		// TODO check source IP/session to either login/switch
		ctx.HTML(http.StatusOK, "login.gohtml", nil)
	})

	r.GET("/login", LoginHandler(s.OIDCProvider))
	r.GET("/callback", CallbackHandler(s.OIDCProvider, "/patch"))
	r.GET("/patch", IsAuthenticatedMiddleware, patchHandler(s))

	r.GET("/switch", IsAuthenticatedMiddleware, switchVLANHandler(s))
	r.POST("/switch", IsAuthenticatedMiddleware, switchVLANSubmitHandler(s))
	r.GET("/switch/success", IsAuthenticatedMiddleware, switchVLANSuccessHandler(s))

	// TODO maybe add logout route

	r.GET("/liveness", livenessHandler(s))
	r.GET("/readiness", readinessHandler(s))

	s.Log.Info().Str("addr", listen).Msg("Listening...")
	return http.ListenAndServe(listen, r)
}

// executed every few seconds in order to check the container status.
func livenessHandler(s *Server) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		if err := s.DB.PingContext(ctx.Request.Context()); err != nil {
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

func renderError(ctx *gin.Context, page string, code int, msg string) {
	ctx.HTML(code, page, gin.H{
		"error": msg,
	})
}
