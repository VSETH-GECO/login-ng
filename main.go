package main

import (
	"context"
	"flag"
	"io"
	"os"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/VSETH-GECO/login-ng/server"
)

var (
	levelFlag  = flag.String("log-level", "info", "Sets the verbosity of the logger. One of: trace, debug, info, warn, error, fatal, panic")
	writerFlag = flag.String("log-format", "console", "Log output format. One of: console, json.")

	mysqlServer   = flag.String("mysql-server", os.Getenv("MYSQL_DB_SERVER"), "MySQL server address (required)")
	mysqlPort     = flag.String("mysql-port", os.Getenv("MYSQL_DB_PORT"), "MySQL port number (required)")
	mysqlDatabase = flag.String("mysql-name", os.Getenv("MYSQL_DB_NAME"), "MySQL database name (required)")
	mysqlUser     = flag.String("mysql-user", os.Getenv("MYSQL_DB_USER"), "MySQL database user (required)")
	mysqlPassword = flag.String("mysql-pw", os.Getenv("MYSQL_DB_PW"), "MySQL database user password (required)")

	oidcIssuer       = flag.String("oidc-issuer", os.Getenv("OIDC_ISSUER"), "Geco OIDC Provider (required)")
	oidcRedirectURL  = flag.String("oidc-redirect-url", os.Getenv("OIDC_REDIRECT_URL"), "Geco OIDC Redirect URL (required)")
	oidcClientID     = flag.String("oidc-client-id", os.Getenv("OIDC_CLIENT_ID"), "Geco OIDC Client ID (required)")
	oidcClientSecret = flag.String("oidc-client-secret", os.Getenv("OIDC_CLIENT_SECRET"), "Geco OIDC Client secret (required)")

	gecoAPILanID                 = flag.String("geco-lan-id", os.Getenv("GECO_LAN_ID"), "Geco LAN ID (required). The id of the LAN event instance on the website.")
	gecoAPIUserstatusEndpointFmt = flag.String("geco-userstatus-endpoint", os.Getenv("GECO_USERSTATUS_ENDPOINT"), "Geco user status endpoint format (required). Geco API endpoint as specified on https://geco.ethz.ch/api/v1#/paths/api-v1-lan_parties-id--me/get.")

	sessionSecret = flag.String("session-secret", os.Getenv("SESSION_SECRET"), "Session secret (required). It is recommended to use a session key with 32 or 64 bytes.")

	listenFlag = flag.String("listen", ":8080", "Where the HTTP server should listen.")
)

func main() {
	flag.Parse()
	args := map[string]string{
		"mysql-server":             *mysqlServer,
		"mysql-port":               *mysqlPort,
		"mysql-name":               *mysqlDatabase,
		"mysql-user":               *mysqlUser,
		"mysql-pw":                 *mysqlPassword,
		"oidc-issuer":              *oidcIssuer,
		"oidc-redirect-url":        *oidcRedirectURL,
		"oidc-client-id":           *oidcClientID,
		"oidc-client-secret":       *oidcClientSecret,
		"geoc-lan-id":              *gecoAPILanID,
		"geoc-userstatus-endpoint": *gecoAPIUserstatusEndpointFmt,
		"session-secret":           *sessionSecret,
	}
	argMissing := false
	for argName, argValue := range args {
		if len(argValue) == 0 {
			argMissing = true
			log.Error().Msgf("missing required argument: %s", argName)
		}
	}
	if argMissing {
		log.Fatal().Msg("missing required arguments")
	}

	// Set up the logger.
	lvl, err := zerolog.ParseLevel(*levelFlag)
	if err != nil {
		log.Fatal().Err(err).Msgf("Could not parse log-level flag: '%s'.", *levelFlag)
	}
	zerolog.SetGlobalLevel(lvl)
	var w io.Writer = os.Stderr
	switch *writerFlag {
	case "console":
		w = zerolog.ConsoleWriter{Out: w}
	case "json":
		// This is the default of zerolog.
	}
	logger := zerolog.New(w).With().Timestamp().Caller().Logger()

	// Connect to and migrate database
	openDBCtx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	db, err := server.OpenDB(openDBCtx, *mysqlUser, *mysqlPassword, *mysqlServer, *mysqlPort, *mysqlDatabase)
	if err != nil {
		logger.
			Fatal().
			Err(err).
			Str("server", *mysqlServer).
			Str("port", *mysqlPort).
			Str("database", *mysqlDatabase).
			Str("user", *mysqlUser).
			Msg("Failed to open connection to DB.")
	}
	cancel()
	logger.Info().Msgf("Connected to database: %v:%v/%v", *mysqlServer, *mysqlPort, *mysqlDatabase)

	// Create OIDC provider
	oidcProvider, err := server.NewOIDCProvider(
		logger.With().Str("component", "oidc").Logger(),
		*oidcIssuer,
		*oidcRedirectURL,
		*oidcClientID,
		*oidcClientSecret,
	)
	if err != nil {
		logger.
			Fatal().
			Err(err).
			Str("server", *mysqlServer).
			Str("port", *mysqlPort).
			Str("database", *mysqlDatabase).
			Str("user", *mysqlUser).
			Msg("Failed to create OIDC provider.")
	}

	// assemble geco API config
	gecoAPIConfig := &server.GecoAPIConfig{
		LanID:                 *gecoAPILanID,
		UserstatusEndpointFmt: *gecoAPIUserstatusEndpointFmt,
	}

	// Setup server
	sl := logger.With().Str("component", "server").Logger()
	s := server.Server{
		Log:           sl,
		DB:            db,
		OIDCProvider:  oidcProvider,
		GecoAPIConfig: gecoAPIConfig,
		SessionSecret: *sessionSecret,
	}

	logger.Fatal().Err(s.ListenAndServe(*listenFlag)).Msg("Failed.")
}
