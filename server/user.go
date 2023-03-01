package server

import (
	"crypto/tls"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

const (
	sessionUserSub         = "sub"
	sessionUserName        = "username"
	sessionUserAccessToken = "access_token"
)

type GecoAPIConfig struct {
	LanID                 string
	UserstatusEndpointFmt string
}

// see https://geco.ethz.ch/api/v1#/paths/api-v1-lan_parties-id--me/get
func (s *Server) userIsCheckedin(ctx *gin.Context) error {
	session := sessions.Default(ctx)
	sub := session.Get(sessionUserSub).(string)
	log := s.Log.With().Str("sub", sub).Logger()

	client := &http.Client{Transport: &http.Transport{
		TLSClientConfig: &tls.Config{},
	}}

	accessToken := session.Get(sessionUserAccessToken).(string)
	userstatusURL := fmt.Sprintf(s.GecoAPIConfig.UserstatusEndpointFmt, s.GecoAPIConfig.LanID)
	req, err := http.NewRequestWithContext(ctx.Request.Context(), http.MethodGet, userstatusURL, nil)
	if err != nil {
		log.Error().Err(err).Msg("failed to create user status request")
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := client.Do(req)
	if err != nil {
		log.Error().Err(err).Msg("Failed to send user status request.")
		return err
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Error().Err(err).Msg("Failed to read body.")
		return err
	}

	switch resp.StatusCode {
	case http.StatusOK: // 200
		return nil
	case http.StatusUnprocessableEntity: // 422
		log.Info().Msg("No ticket or not checked-in")
		return errors.New("Please assign a ticket to your account or check-in first.")
	default: // 401 or 404
		log.
			Error().
			Int("code", resp.StatusCode).
			Str("body", string(body)).
			Msg("Failed to get user status.")
		return errors.New("Failed to check user status. Please try again.")
	}
}
