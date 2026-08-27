package server

import (
	"net/http"
	"strings"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

func connectHandler(s *Server) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		err := s.userIsCheckedIn(ctx)
		if err != nil {
			renderError(ctx, "error.gohtml", http.StatusForbidden, err.Error())
			return
		}

		err = s.patchIntoSwitchVLAN(ctx)
		if err != nil {
			renderError(ctx, "error.gohtml", http.StatusInternalServerError, "Failed to patch into the network.")
			return
		}

		session := sessions.Default(ctx)
		ctx.HTML(http.StatusOK, "success.gohtml", gin.H{
			"username": session.Get(sessionUserName),
		})
	}
}

func (s *Server) patchIntoSwitchVLAN(ctx *gin.Context) error {
	// find source switch
	userIP := resolveUserIP(ctx.Request)
	up, err := s.locateUser(ctx.Request.Context(), userIP)
	if err != nil {
		s.Log.Error().Err(err).Str("user IP", userIP).Msg("failed to find source switch")
		renderError(ctx, "index.gohtml", http.StatusInternalServerError, "Unable to locate the switch the user is connected to.")
		return err
	}

	// map switch to vlan
	targetVLAN, err := s.getSwitchVLAN(ctx.Request.Context(), up.switchIP)
	if err != nil {
		s.Log.Error().Err(err).Str("switch IP", up.switchIP).Msg("VLAN for switch not found")
		renderError(ctx, "index.gohtml", http.StatusInternalServerError, "Unkown switch IP")
		return err
	}

	return s.patch(ctx, up.userMAC, targetVLAN)
}

func (s *Server) patch(ctx *gin.Context, userMAC string, targetVLAN int) error {
	// create bounce job
	err := s.createNewBounceJob(ctx.Request.Context(), userMAC, targetVLAN)
	if err != nil {
		s.Log.Error().Err(err).
			Str("user MAC", userMAC).
			Int("target VLAN", targetVLAN).
			Msg("failed to create a new bounce job")
		renderError(ctx, "index.gohtml", http.StatusInternalServerError, "Internal Server Error: Please contact the support.")
		return err
	}

	// log
	session := sessions.Default(ctx)
	username := session.Get(sessionUserName).(string)
	err = s.createNewLoginLog(ctx.Request.Context(), username, userMAC)
	if err != nil {
		s.Log.Error().Err(err).
			Str("username", username).
			Str("user MAC", userMAC).
			Msg("failed to log patch")
		// ignore error as its only logging
	}

	return nil
}

func resolveUserIP(request *http.Request) string {
	userIP := strings.Split(request.RemoteAddr, ":")[0]
	if xff, ok := request.Header["X-Forwarded-For"]; ok {
		userIP = xff[0]
	}
	return userIP
}

func switchVLANHandler(s *Server) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		// TODO
		ctx.String(http.StatusInternalServerError, "unimplemented")
	}
}

func switchVLANSubmitHandler(s *Server) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		// TODO
		ctx.String(http.StatusInternalServerError, "unimplemented")
	}
}

func switchVLANSuccessHandler(s *Server) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		// TODO
		ctx.String(http.StatusInternalServerError, "unimplemented")
	}
}
