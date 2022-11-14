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

	gecoAPIurl = flag.String("geco-api-url", os.Getenv("GECO_API_URL"), "Geco API URL (required)")
	gecoAPIkey = flag.String("geco-api-key", os.Getenv("GECO_API_KEY"), "Geco API key (required)")

	listenFlag = flag.String("listen", ":8080", "Where the HTTP server should listen.")
)

func main() {
	flag.Parse()
	if len(*mysqlServer) == 0 || len(*mysqlPort) == 0 || len(*mysqlDatabase) == 0 || len(*mysqlUser) == 0 || len(*mysqlPassword) == 0 || len(*gecoAPIurl) == 0 || len(*gecoAPIkey) == 0 {
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
		logger.Fatal().Err(err).Str("server", *mysqlServer).Str("port", *mysqlPort).Str("database", *mysqlDatabase).Str("user", *mysqlUser).Msg("Failed to open connection to DB.")
	}
	cancel()
	logger.Info().Msgf("Connected to database: %v:%v/%v", *mysqlServer, *mysqlPort, *mysqlDatabase)

	// Setup server
	sl := logger.With().Str("component", "server").Logger()
	s := server.Server{Log: sl, DB: db, GecoAPIurl: *gecoAPIurl, GecoAPIkey: *gecoAPIkey}

	logger.Fatal().Err(s.ListenAndServe(*listenFlag))
}
