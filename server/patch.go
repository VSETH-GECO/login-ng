package server

import (
	"net/http"
	"strconv"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

const (
	logonVLAN = 499
)

func RequiredCheckedIn(s *Server) func(ctx *gin.Context) {
	return func(ctx *gin.Context) {
		// derive retry URL from the current request path
		retryURL := ctx.Request.URL.Path
		err := s.userIsCheckedIn(ctx)
		if err != nil {
			renderError(ctx, "error.gohtml", http.StatusForbidden, err.Error(), retryURL)
		} else {
			ctx.Next()
		}
	}
}

func connectHandler(s *Server) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		err := s.patchIntoSwitchVLAN(ctx)
		if err != nil {
			renderError(ctx, "error.gohtml", http.StatusInternalServerError, "Failed to connect.", "/connect")
			return
		}

		session := sessions.Default(ctx)
		ctx.HTML(http.StatusOK, "success.gohtml", gin.H{
			"connecting": true,
			"username":   session.Get(sessionUserName),
		})
	}
}

func disconnectHandler(s *Server) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		err := s.patchIntoLogonVLAN(ctx)
		if err != nil {
			renderError(ctx, "error.gohtml", http.StatusInternalServerError, "Failed to disconnect.", "/disconnect")
			return
		}

		session := sessions.Default(ctx)
		ctx.HTML(http.StatusOK, "success.gohtml", gin.H{
			"connecting": false,
			"username":   session.Get(sessionUserName),
		})
	}
}

func (s *Server) patchIntoSwitchVLAN(ctx *gin.Context) error {
	// find source switch
	userIP := ctx.ClientIP()
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

func (s *Server) patchIntoLogonVLAN(ctx *gin.Context) error {
	userIP := ctx.ClientIP()
	up, err := s.locateUser(ctx.Request.Context(), userIP)
	if err != nil {
		s.Log.Error().Err(err).Str("user IP", userIP).Msg("failed to find source switch")
		renderError(ctx, "index.gohtml", http.StatusInternalServerError, "Unable to locate the switch the user is connected to.")
		return err
	}

	return s.patch(ctx, up.userMAC, logonVLAN)
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
	username, ok := session.Get(sessionUserName).(string)
	if !ok || username == "" {
		s.Log.Error().Str("user MAC", userMAC).Msg("username missing from session during login log")
		return nil
	}
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

func switchVLANHandler(s *Server) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		userIP := ctx.ClientIP()
		up, err := s.locateUser(ctx.Request.Context(), userIP)
		if err != nil {
			s.Log.Error().Err(err).Str("user IP", userIP).Msg("failed to find source switch")
			renderError(ctx, "error.gohtml", http.StatusInternalServerError, "Unable to locate the switch the user is connected to.", "/switch")
			return
		}

		primaryVLAN, err := s.getSwitchVLAN(ctx.Request.Context(), up.switchIP)
		if err != nil {
			s.Log.Error().Err(err).Str("switch IP", up.switchIP).Msg("VLAN for switch not found")
			renderError(ctx, "error.gohtml", http.StatusInternalServerError, "Unknown switch IP.", "/switch")
			return
		}

		vlans, err := s.getAvailableVLANs(ctx.Request.Context(), primaryVLAN)
		if err != nil {
			s.Log.Error().Err(err).Int("primary VLAN", primaryVLAN).Msg("failed to get available VLANs")
			renderError(ctx, "error.gohtml", http.StatusInternalServerError, "Failed to load available VLANs.", "/switch")
			return
		}

		session := sessions.Default(ctx)
		ctx.HTML(http.StatusOK, "switch.gohtml", gin.H{
			"vlans":    vlans,
			"username": session.Get(sessionUserName),
		})
	}
}

func switchVLANSubmitHandler(s *Server) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		// parse submitted VLAN ID
		vlanStr := ctx.PostForm("vlan")
		targetVLAN, err := strconv.Atoi(vlanStr)
		if err != nil || vlanStr == "" {
			renderError(ctx, "error.gohtml", http.StatusBadRequest, "Invalid VLAN selection.", "/switch")
			return
		}

		// resolve user IP → switch IP + MAC
		userIP := ctx.ClientIP()
		up, err := s.locateUser(ctx.Request.Context(), userIP)
		if err != nil {
			s.Log.Error().Err(err).Str("user IP", userIP).Msg("failed to find source switch")
			renderError(ctx, "error.gohtml", http.StatusInternalServerError, "Unable to locate the switch the user is connected to.", "/switch")
			return
		}

		// get primary VLAN for this switch
		primaryVLAN, err := s.getSwitchVLAN(ctx.Request.Context(), up.switchIP)
		if err != nil {
			s.Log.Error().Err(err).Str("switch IP", up.switchIP).Msg("VLAN for switch not found")
			renderError(ctx, "error.gohtml", http.StatusInternalServerError, "Unknown switch IP.", "/switch")
			return
		}

		// validate submitted VLAN is in the allowed list
		vlans, err := s.getAvailableVLANs(ctx.Request.Context(), primaryVLAN)
		if err != nil {
			s.Log.Error().Err(err).Int("primary VLAN", primaryVLAN).Msg("failed to get available VLANs")
			renderError(ctx, "error.gohtml", http.StatusInternalServerError, "Failed to load available VLANs.", "/switch")
			return
		}
		allowed := false
		for _, v := range vlans {
			if v.VLANID == targetVLAN {
				allowed = true
				break
			}
		}
		if !allowed {
			renderError(ctx, "error.gohtml", http.StatusBadRequest, "Selected VLAN is not available.", "/switch")
			return
		}

		// create bounce job
		if err := s.patch(ctx, up.userMAC, targetVLAN); err != nil {
			return
		}

		ctx.Redirect(http.StatusSeeOther, "/switch/success")
	}
}

func switchVLANSuccessHandler(s *Server) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		session := sessions.Default(ctx)
		ctx.HTML(http.StatusOK, "success.gohtml", gin.H{
			"switching": true,
			"username":  session.Get(sessionUserName),
		})
	}
}
