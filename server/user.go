package server

import (
	"context"
	"crypto/tls"
	"errors"
	"net/http"
	"net/url"
	"strings"
)

type authResp struct {
	Code    int    `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
	Success bool   `json:"success,omitempty"`
	String  string `json:"error,omitempty"`
}

/*
	curl \
		-v \
		-X POST \
		--data-urlencode 'username=flbuetle password=INSERT' \
		-H 'X-API-KEY: INSERT' \
		'https://geco.ethz.ch/api/v2/auth'
*/
func (s *Server) authenticate(ctx context.Context, username, password string) error {
	client := &http.Client{Transport: &http.Transport{
		TLSClientConfig: &tls.Config{},
	}}

	data := url.Values{
		"username": []string{username},
		"password": []string{password},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.GecoAPIurl, strings.NewReader(data.Encode()))
	if err != nil {
		s.Log.Error().Err(err).Msg("failed to create request")
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-API-KEY", s.GecoAPIkey)

	resp, err := client.Do(req)
	if err != nil {
		s.Log.Error().Err(err).Msg("failed to send auth request")
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		s.Log.Error().Int("code", resp.StatusCode).Msg("auth server responded wrong status code")
		return errors.New("invalid status code")
	}

	/* TODO anythin to check here?
	var ar authResp
	err = json.NewDecoder(resp.Body).Decode(&ar)
	if err != nil {
		s.Log.Error().Err(err).Msg("failed to decode response")
		return err
	}

	if ar.Code != http.StatusOK {
		s.Log.Error().Int("code", ar.Code).Str("message", ar.Message).Msg("auth response has wrong status code")
		return errors.New("invalid status code")
	}
	*/

	s.Log.Info().Msg("successfully authenticated user")
	return nil
}
