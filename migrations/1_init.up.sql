-- Create initial version of the database (see: https://github.com/rubenv/sql-migrate#writing-migrations)
-- +migrate Up
CREATE TABLE login_logs (
    id INTEGER NOT NULL AUTO_INCREMENT PRIMARY KEY,
    username varchar(255) NOT NULL,
    mac varchar(32) NOT NULL,
    last_update TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- +migrate Down
DROP TABLE login_logs;
