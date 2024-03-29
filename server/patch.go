package server

import (
	"net/http"
	"strings"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

func patchHandler(s *Server) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		err := s.userIsCheckedin(ctx)
		if err != nil {
			renderError(ctx, "patch.gohtml", http.StatusForbidden, err.Error())
			return
		}

		err = s.patchIntoVLAN(ctx)
		if err != nil {
			renderError(ctx, "patch.gohtml", http.StatusInternalServerError, "Failed to patch into the network.")
			return
		}

		session := sessions.Default(ctx)
		ctx.HTML(http.StatusOK, "success.gohtml", gin.H{
			"username": session.Get(sessionUserName),
		})
	}
}

func (s *Server) patchIntoVLAN(ctx *gin.Context) error {
	// find source switch
	userIP := strings.Split(ctx.Request.RemoteAddr, ":")[0]
	if xff, ok := ctx.Request.Header["X-Forwarded-For"]; ok {
		userIP = xff[0]
	}
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

	// create bounce job
	err = s.createNewBounceJob(ctx.Request.Context(), up.userMAC, targetVLAN)
	if err != nil {
		s.Log.Error().Err(err).
			Str("user MAC", up.userMAC).
			Int("target VLAN", targetVLAN).
			Msg("failed to create a new bounce job")
		renderError(ctx, "index.gohtml", http.StatusInternalServerError, "Internal Server Error: Please contact the support.")
		return err
	}

	// log
	session := sessions.Default(ctx)
	username := session.Get(sessionUserName).(string)
	err = s.createNewLoginLog(ctx.Request.Context(), username, up.userMAC)
	if err != nil {
		s.Log.Error().Err(err).
			Str("username", username).
			Str("user MAC", up.userMAC).
			Msg("failed to log login")
		// ignore error as its only logging
	}

	return nil
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
