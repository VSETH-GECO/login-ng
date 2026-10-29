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

CREATE TABLE login_logs (
    id INTEGER NOT NULL AUTO_INCREMENT PRIMARY KEY,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    username varchar(255) NULL,
    mac varchar(255) NULL,
    KEY idx_date (`created_at`),
    KEY idx_name (`username`),
    KEY idx_mac (`mac`)
);

CREATE TABLE bouncer_switch_map (
    id INTEGER NOT NULL AUTO_INCREMENT PRIMARY KEY,
    primary_vlan INTEGER NOT NULL UNIQUE,
    hostname varchar(255) NOT NULL,
    location varchar(255) NULL,
    KEY idx_vlan (`primary_vlan`)
);

CREATE TABLE bouncer_switch_ip (
    id INTEGER NOT NULL AUTO_INCREMENT PRIMARY KEY,
    switch_id INTEGER NOT NULL REFERENCES bouncer_switch_map(id),
    ip varchar(255) NOT NULL,
    KEY idx_ip (`ip`),
    KEY idx_switch_id (`switch_id`)
);

CREATE TABLE bouncer_vlan (
    id INTEGER NOT NULL AUTO_INCREMENT PRIMARY KEY,
    name varchar(255) NOT NULL,
    description varchar(255) NOT NULL,
    vlan_id INTEGER NOT NULL UNIQUE,
    ip_range varchar(255) NOT NULL UNIQUE
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

INSERT INTO
    bouncer_switch_map(id, primary_vlan, hostname, location)
VALUES
    (1, 527, "user27.lan.geco.ethz.ch", "test");

INSERT INTO
    bouncer_switch_ip(switch_id, ip)
VALUES
    (1, "10.233.254.27");

INSERT INTO
    bouncer_vlan(name, description, vlan_id, ip_range)
VALUES
    ("User 27", "Primary VLAN for user 27", 527, "10.233.27.0/24"),
    ("Shared 1", "Shared VLAN 1", 490, "10.233.90.0/24"),
    ("Shared 2", "Shared VLAN 2", 491, "10.233.91.0/24");

-- +migrate Down
DROP TABLE bouncer_vlan;

DROP TABLE lease4;

DROP TABLE radacct;

DROP TABLE users;

DROP TABLE bouncer_jobs;

DROP TABLE login_logs;

DROP TABLE bouncer_switch_map;

DROP TABLE bouncer_switch_ip;
