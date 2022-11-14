# Login for PolyLAN lan

Small go app for a simple login page to authenticate users during the Lan.

## Rationale

When a user first connects to the PolyLAN network, they are patched into a special VLAN where only this app is reachable. The user can then login with their GeCo credentials and can access the internet.

This works, as this app
* authenticates the user,
* finds the switch the user is plugged into,
* resolves the VLAN assigned to this switch and then
* creates a bouncer job.

The bouncer (dedicated app) is looking for bounce jobs and bounces the port on the switch the user is connected to such that the user ends up in the correct VLAN with access to the internet.

## Development

```bash
$ docker-compose up -d db
$ go install github.com/rubenv/sql-migrate/...@latest
$ sql-migrate up -config test-migrations/dbconfig.yml
$ go run . \
    -mysql-server localhost \
    -mysql-port 3306 \
    -mysql-name freeradius \
    -mysql-user login \
    -mysql-pw login \
    -geco-api-url 'https://geco.ethz.ch/api/v2/auth' \
    -geco-api-key blub \
    -log-level debug \
    -log-format console
```

Connect to the database manually

```bash
mysql -h localhost -P 3306 --database freeradius -ulogin -plogin
$ show tables;
```

## Debug

Use the debug configuration in `.vscode/launch.json`.
