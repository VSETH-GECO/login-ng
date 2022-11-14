-- Create initial version of the database (see: https://github.com/rubenv/sql-migrate#writing-migrations)
-- +migrate Up
CREATE TABLE bouncer_jobs (
    id INTEGER NOT NULL AUTO_INCREMENT PRIMARY KEY,
    clientMAC VARCHAR(32) NOT NULL,
    targetVLAN SMALLINT NOT NULL,
    last_update TIMESTAMP DEFAULT CURRENT_TIMESTAMP NOT NULL,
    retires INT DEFAULT 0 NOT NULL
);

CREATE TABLE users (
    mac VARCHAR(255) NOT NULL,
    switch VARCHAR(255) NOT NULL,
    ip VARCHAR(255) NOT NULL
);

CREATE TABLE radacct (
    username VARCHAR(32) NOT NULL,
    nasipaddress VARCHAR(32) NOT NULL,
    acctstoptime VARCHAR(32) DEFAULT NULL
);

CREATE TABLE lease4 (
    address INT NOT NULL,
    hwaddr VARCHAR(32) NOT NULL
);

-- 127.0.0.1 -> 127 * 2**24 + 0 * 2**16 + 0 * 2**8 + 1 * 2**0
-- 61:62:63:64:65:66 -> abcdef
INSERT INTO
    lease4(address, hwaddr)
VALUES
    (2130706433, "abcdef");

INSERT INTO
    radacct(username, nasipaddress, acctstoptime)
VALUES
    ("616263646566", "10.233.254.27", NULL);

-- +migrate Down
DROP TABLE lease4;

DROP TABLE radacct;

DROP TABLE users;

DROP TABLE bouncer_jobs;
