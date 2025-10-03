package server

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"io"
	"net/http"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
	"golang.org/x/oauth2"
)

const (
	oidcScopePolylan = "user:lan:read"
)

type OIDCProvider struct {
	log zerolog.Logger
	*oidc.Provider
	oauth2.Config
}

func NewOIDCProvider(log zerolog.Logger, issuer, redirectURL, clientID, clientSecret string) (*OIDCProvider, error) {
	provider, err := oidc.NewProvider(context.Background(), issuer)
	if err != nil {
		return nil, err
	}

	oauth2config := oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		RedirectURL:  redirectURL,
		// Discovery returns the OAuth2 endpoints.
		Endpoint: provider.Endpoint(),
		// "openid" is a required scope for OpenID Connect flows.
		Scopes: []string{oidc.ScopeOpenID, oidcScopePolylan},
	}

	return &OIDCProvider{
		log:      log,
		Provider: provider,
		Config:   oauth2config,
	}, nil
}

func (a *OIDCProvider) verifyIDToken(ctx context.Context, token *oauth2.Token) (*oidc.IDToken, error) {
	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok {
		return nil, errors.New("no id_token field in oauth2 token")
	}

	oidcConfig := &oidc.Config{
		ClientID: a.ClientID,
	}

	return a.Verifier(oidcConfig).Verify(ctx, rawIDToken)
}

func LoginHandler(auth *OIDCProvider) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		state, err := randString(16)
		if err != nil {
			auth.log.Error().Err(err).Msg("failed to generate state")
			renderError(ctx, "index.gohtml", http.StatusInternalServerError, "Internal error")
			return
		}
		nonce, err := randString(16)
		if err != nil {
			auth.log.Error().Err(err).Msg("failed to generate nonce")
			renderError(ctx, "index.gohtml", http.StatusInternalServerError, "Internal error")
			return
		}

		session := sessions.Default(ctx)
		session.Set("state", state)
		session.Set("nonce", nonce)
		if err := session.Save(); err != nil {
			auth.log.Error().Err(err).Msg("failed to save session")
			renderError(ctx, "index.gohtml", http.StatusInternalServerError, err.Error())
			return
		}

		ctx.Redirect(http.StatusTemporaryRedirect, auth.AuthCodeURL(state, oidc.Nonce(nonce)))
	}
}

func CallbackHandler(auth *OIDCProvider, postLoginRedirectURL string) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		session := sessions.Default(ctx)
		if ctx.Query("state") != session.Get("state") {
			auth.log.Error().Msg("invalid state parameter")
			renderError(ctx, "index.gohtml", http.StatusBadRequest, "Invalid state parameter.")
			return
		}

		token, err := auth.Exchange(ctx.Request.Context(), ctx.Query("code"))
		if err != nil {
			auth.log.Error().Err(err).Msg("failed to exchange code")
			renderError(ctx, "index.gohtml", http.StatusUnauthorized, "Failed to exchange an authorization code for a token.")
			return
		}

		idToken, err := auth.verifyIDToken(ctx.Request.Context(), token)
		if err != nil {
			auth.log.Error().Err(err).Msg("failed to verify token")
			renderError(ctx, "index.gohtml", http.StatusInternalServerError, "Failed to verify ID Token.")
			return
		}

		if idToken.Nonce != session.Get("nonce") {
			auth.log.Error().Msg("invalid nonce parameter")
			renderError(ctx, "index.gohtml", http.StatusBadRequest, "Invalid nonce parameter.")
			return
		}

		var claims struct {
			Username string `json:"username"`
		}
		if err := idToken.Claims(&claims); err != nil {
			auth.log.Error().Msg("failed to parse custom claims")
			renderError(ctx, "index.gohtml", http.StatusInternalServerError, "Failed to get parse custom claims.")
			return
		}

		session.Set(sessionUserAccessToken, token.AccessToken)
		session.Set(sessionUserSub, idToken.Subject)
		session.Set(sessionUserName, claims.Username)
		if err := session.Save(); err != nil {
			auth.log.Error().Err(err).Msg("failed to save session")
			renderError(ctx, "index.gohtml", http.StatusInternalServerError, err.Error())
			return
		}

		ctx.Redirect(http.StatusTemporaryRedirect, postLoginRedirectURL)
	}
}

func LogoutHandler(auth *OIDCProvider) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		session := sessions.Default(ctx)

		session.Clear()

		if err := session.Save(); err != nil {
			auth.log.Error().Err(err).Msg("failed to save session")
			renderError(ctx, "index.gohtml", http.StatusInternalServerError, err.Error())
			return
		}

		ctx.Redirect(http.StatusTemporaryRedirect, "/")
	}
}

func IsAuthenticatedMiddleware(ctx *gin.Context) {
	if sessions.Default(ctx).Get(sessionUserSub) == nil {
		ctx.Redirect(http.StatusSeeOther, "/")
	} else {
		ctx.Next()
	}
}

func randString(nByte int) (string, error) {
	b := make([]byte, nByte)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
