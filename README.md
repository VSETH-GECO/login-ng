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
docker-compose up -d db
go install github.com/rubenv/sql-migrate/...@latest
sql-migrate up -config test-migrations/dbconfig.yml
go run . \
    -mysql-server localhost \
    -mysql-port 3306 \
    -mysql-name freeradius \
    -mysql-user login \
    -mysql-pw login \
    -oidc-issuer https://geco.ethz.ch/ \
    -oidc-redirect-url https://localhost:8080/callback \
    -oidc-client-id login-ng \
    -oidc-client-secret topsecret \
    -geco-lan-id=1 \
    -geco-userstatus-endpoint=https://geco.ethz.ch/api/v1/lan_parties/%s/me \
    -session-secret abcdef \
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

## Examples

### Example ID token

The `access_token` can be used to query the GECo API.

```json
{
    "OAuth2Token": {
        "access_token": "*REDACTED*",
        "token_type": "Bearer",
        "expiry": "2023-02-17T20:48:09.675152642+01:00"
    },
    "IDToken": {
        "Issuer": "https://geco.ethz.ch/",
        "Audience": [
            "Jv-2yo6kJv_NuqiN2iCAAAKzIky3rTH8ZzzXqiFTjY4"
        ],
        "Subject": "1607",
        "Expiry": "2023-02-17T18:50:09+01:00",
        "IssuedAt": "2023-02-17T18:48:09+01:00",
        "Nonce": "P3HlDxq3jRGRhzQlc02qJQ",
        "AccessTokenHash": ""
    }
}
```

### Example GECo API user status response

See <https://lan.h4ck.ch/api/v1#/paths/api-v1-lan_parties-id--me/get>

* Ticket bought and checked in (status code 200):

```json
{
  "user": {
    "id": 1607,
    "username": "aponax"
  },
  "seat": {
    "id": 14,
    "name": "14"
  }
}
```

* No ticket or not checked in (status code 422):

```json
{
  "message": "User has no ticket assigned for this LanParty"
}
```

## Network

To make this app reachable from within the special VLAN you have to configure the HAProxy on the PfSense at two locations:

* Set`Services > HAProxy > Backend > redirect (edit) > Advanced settings >  Backend pass thru` to `http-request redirect code 301 location https://login-ng.lan.geco.ethz.ch`.
* Whitelist the app in `Services > HAProxy > Fronteend > HTTP (edit) > Default backend` under `Access Control lists` and `Actions`.
