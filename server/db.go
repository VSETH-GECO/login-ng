package server

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	_ "github.com/go-sql-driver/mysql"
	migrate "github.com/rubenv/sql-migrate"
)

type db struct {
	*sql.DB
}

type userProperties struct {
	userIP   string
	userMAC  string
	switchIP string
}

// OpenDB creates a DB connection and performs all migrations.
func OpenDB(ctx context.Context, mysqlUser, mysqlPassword, mysqlServer, mysqlPort, mysqlDatabase string) (db, error) {
	mariadbURL := fmt.Sprintf("%v:%v@tcp(%v:%v)/%v?multiStatements=true&parseTime=true", mysqlUser, mysqlPassword, mysqlServer, mysqlPort, mysqlDatabase)
	mardiadb, err := sql.Open("mysql", mariadbURL)
	if err != nil {
		return db{}, fmt.Errorf("failed to open connection to DB: %w", err)
	}

	migrations := migrate.FileMigrationSource{Dir: "migrations"}
	_, err = migrate.Exec(mardiadb, "mysql", migrations, migrate.Up)
	if err != nil {
		return db{}, fmt.Errorf("failed to apply migrations: %w", err)
	}

	return db{mardiadb}, nil
}

const qGetUserProperties = `
SELECT INET_NTOA(l.address) AS user_ip, u.username AS user_mac, u.nasipaddress AS switch_ip
FROM (radacct u join lease4 l ON((u.username = lower(hex(l.hwaddr)))))
WHERE (u.acctstoptime IS NULL) AND INET_NTOA(l.address)=?
GROUP BY user_mac, switch_ip, user_ip;`

func (s *Server) locateUser(ctx context.Context, userIP string) (*userProperties, error) {
	up := new(userProperties)
	err := s.DB.QueryRowContext(ctx, qGetUserProperties, userIP).Scan(&up.userIP, &up.userMAC, &up.switchIP)
	if err != nil {
		if err == sql.ErrNoRows {
			s.Log.Error().Err(err).Str("user ip", userIP).Msg("failed to locate user")
			return nil, errors.New("user not found")
		}
		s.Log.Error().Err(err).
			Str("user IP", userIP).
			Msg("Failed to fetch user properties")
		return nil, fmt.Errorf("failed to get user properties: %w", err)
	}

	return up, nil
}

const qInsertBounceJob = `INSERT INTO bouncer_jobs(clientMAC, targetVLAN) VALUES(?, ?);`

func (s *Server) createNewBounceJob(ctx context.Context, clientMAC string, targetVLAN int) error {
	_, err := s.DB.ExecContext(ctx, qInsertBounceJob, clientMAC, targetVLAN)
	if err != nil {
		s.Log.Error().Err(err).
			Str("clientMac", clientMAC).
			Int("targetVlan", targetVLAN).
			Msg("Failed to insert bounce job into database.")
		return err
	}
	return nil
}

const qInsertLoginLog = `INSERT INTO login_logs(username, mac) VALUES(?, ?);`

func (s *Server) createNewLoginLog(ctx context.Context, username string, clientMAC string) error {
	_, err := s.DB.ExecContext(ctx, qInsertLoginLog, username, clientMAC)
	if err != nil {
		s.Log.Error().Err(err).
			Str("username", username).
			Str("clientMac", clientMAC).
			Msg("Failed to insert login log into database.")
		return err
	}
	return nil
}
