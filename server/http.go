package server

import (
	"errors"
	"net/http"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/gorilla/mux"
	"github.com/rs/zerolog"
)

// Server is the server struct
type Server struct {
	Log        zerolog.Logger
	DB         db
	GecoAPIurl string
	GecoAPIkey string
}

type pageContent struct {
	Error string
}

// ListenAndServe sets up the HTTP server and starts listening
func (s *Server) ListenAndServe(listen string) error {
	r := mux.NewRouter()

	r.Handle("/", http.RedirectHandler("/login", http.StatusSeeOther))

	r.HandleFunc("/login", s.createLoginGetHandler()).Methods(http.MethodGet)
	r.HandleFunc("/login", s.createLoginPostHandler()).Methods(http.MethodPost)
	r.HandleFunc("/login/success", s.createLoginSuccessHandler()).Methods(http.MethodGet)
	r.HandleFunc("/switch", s.createSwitchGetHandler()).Methods(http.MethodGet)
	r.HandleFunc("/switch", s.createSwitchPostHandler()).Methods(http.MethodPost)
	r.HandleFunc("/switch/success", s.createSwitchSuccessHandler()).Methods(http.MethodGet)

	r.HandleFunc("/liveness", s.createLivenessHandler())
	r.HandleFunc("/readiness", s.createReadinessHandler())

	r.PathPrefix("/static/").Handler(http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))

	r.Use(s.loggingMiddleware)

	s.Log.Info().Str("port", listen).Msg("Listening...")
	return http.ListenAndServe(listen, r)
}

func (s *Server) createLoginGetHandler() http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		err := renderTemplate(w, "login.gohtml", nil)
		if err != nil {
			s.Log.Error().Err(err).Msg("failed to render template")
			renderError(w, "login.gohtml", "Internal Server Error: Please contact the support.")
			return
		}
	})
}

func (s *Server) createLoginPostHandler() http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		err := r.ParseForm()
		if err != nil {
			s.Log.Error().Err(err).Msg("failed to parse form")
			renderError(w, "login.gohtml", "Internal Server Error: Please contact the support.")
			return
		}
		form := r.Form

		// authenticate user
		username := form.Get("username")
		password := form.Get("password")
		err = s.authenticate(r.Context(), username, password)
		if err != nil {
			s.Log.Error().Err(err).Msg("failed to authenticate user")
			renderError(w, "login.gohtml", "Username or password incorrect.")
			return
		}

		// find source switch
		// TODO maybe use 'X-Forwarded-For'
		userIP := strings.Split(r.RemoteAddr, ":")[0]
		up, err := s.locateUser(r.Context(), userIP)
		if err != nil {
			s.Log.Error().Err(err).Msg("failed to fetch user properties")
			renderError(w, "login.gohtml", "Unable to locate the switch the user is connected to.")
			return
		}

		// map switch to vlan
		targetVLAN, err := s.translateSwitchIPtoVLAN(r.Context(), up.switchIP)
		if err != nil {
			s.Log.Error().Err(err).Msg("unkown switch ip")
			renderError(w, "login.gohtml", "VLAN for Switch not found.")
			return
		}

		// add as bounce job
		err = s.createNewBounceJob(r.Context(), up.userMAC, targetVLAN)
		if err != nil {
			s.Log.Error().Err(err).Msg("failed to create new bounce job")
			renderError(w, "login.gohtml", "Internal Server Error: Please contact the support.")
			return
		}

		// TODO write to login log table

		http.Redirect(w, r, "/login/success", http.StatusSeeOther)
	})
}

func (s *Server) createLoginSuccessHandler() http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		err := renderTemplate(w, "login_success.gohtml", nil)
		if err != nil {
			s.Log.Error().Err(err).Msg("failed to render template")
			renderError(w, "login.gohtml", "Internal Server Error: Please contact the support.")
			return
		}
	})
}

func (s *Server) createSwitchGetHandler() http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// TODO
		internalError(w, errors.New("unimplemented"))
	})
}

func (s *Server) createSwitchPostHandler() http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// TODO send request to bouncer
		internalError(w, errors.New("unimplemented"))
	})
}

func (s *Server) createSwitchSuccessHandler() http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// TODO
		internalError(w, errors.New("unimplemented"))
	})
}

// executed every few seconds in order to check the container status.
func (s *Server) createLivenessHandler() http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := s.DB.PingContext(r.Context()); err != nil {
			return
		}
		w.Write([]byte("ok"))
	})
}

// executed once to verify the successfull startup of your container.
func (s *Server) createReadinessHandler() http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ready"))
	})
}

func (s *Server) loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.Log.Debug().Str("request URI", r.RequestURI).Msg("Incoming request")
		next.ServeHTTP(w, r)
	})
}

func renderError(w http.ResponseWriter, page string, msg string) {
	w.WriteHeader(http.StatusInternalServerError)
	err := renderTemplate(w, page, &pageContent{Error: msg})
	if err != nil {
		internalError(w, err)
	}
}

func renderTemplate(w http.ResponseWriter, page string, pc *pageContent) error {
	lp := filepath.Join("templates", "layout.gohtml")
	lf := filepath.Join("templates", page)
	tmpl, err := template.New("tmpl").ParseFiles(lp, lf)
	if err != nil {
		return err
	}
	return tmpl.ExecuteTemplate(w, "layout", pc)
}

func internalError(w http.ResponseWriter, err error) {
	http.Error(w, "Internal Server Error: Please contact the support.", http.StatusInternalServerError)
}
