# systemd-service-toggle

`systemd-service-toggle` is a small Go client/server tool for toggling a
configured systemd service remotely.

It uses TLS 1.3 with mutual TLS, an additional password check, and Argon2id for
password storage. The server is intended to run as root because it calls
`systemctl` directly.

## Components

- `systemd-service-toggled`: TLS server/daemon
- `systemd-service-toggle`: TLS client
- `common`: shared config and wire protocol code

The client sends one request and exits. The server accepts one connection at a
time, reads one password frame, verifies it, and toggles the configured systemd
service.

## Security Model

The server is designed to be reachable over an untrusted network, but only with
strict authentication:

- TLS 1.3 only
- mutual TLS is required
- the server verifies the client certificate against `TLS.client-ca-cert`
- the server can additionally verify the client certificate CN with
  `TLS.client-cn`
- the client verifies the server certificate using system CAs plus optional
  `TLS.server-ca-cert`
- passwords are read through a hidden prompt
- passwords are never logged
- password bytes are wiped after use where practical
- the password hash is stored as Argon2id parameters plus salt/hash in YAML
- the `secret` file is written with `0600`
- the server config directory is corrected to `0700`
- the server config and `secret` file are corrected to `0600`

After wrong passwords, the server waits increasingly longer:

```text
delay = wrong_attempts * wrong_attempts * 3 minutes
```

On the tenth wrong password, the daemon disables and stops itself with
`systemctl`.

## Configuration

The client searches:

```text
~/.config/systemd-service-toggle/config-client.yml
/etc/systemd-service-toggle/config-client.yml
```

The server searches:

```text
~/.config/systemd-service-toggle/config-server.yml
/etc/systemd-service-toggle/config-server.yml
```

Example configs are in `config-examples/`.

### Client Config

```yaml
Server:
  address: vpn.example.org
  port: 47112 # optional, default 47112
  timeout: 5 # optional, default 5 seconds

TLS:
  cert: /home/user/.config/systemd-service-toggle/client.crt
  key: /home/user/.config/systemd-service-toggle/client.key
  server-ca-cert: /home/user/.config/systemd-service-toggle/server-ca.crt # optional, extends system CAs
```

### Server Config

```yaml
Server:
  listen: 0.0.0.0 # optional, default 0.0.0.0
  port: 47112 # optional, default 47112
  timeout: 5 # optional, default 5 seconds

TLS:
  cert: /etc/letsencrypt/live/vpn.example.org/fullchain.pem
  key: /etc/letsencrypt/live/vpn.example.org/privkey.pem
  client-ca-cert: /etc/systemd-service-toggle/client-ca.crt
  client-cn: systemd-service-toggle-client # optional, verifies the client certificate CN when set

Service:
  name: example.service
```

## Password Setup

Create or replace the server-side password hash:

```sh
systemd-service-toggled --passwd
```

The command reads the server config, writes `secret` next to it, and exits.

## Development Mode

Run the server in development mode:

```sh
systemd-service-toggled --dev
```

In development mode the server logs to stdout and does not start or stop the
configured service. It only logs what it would toggle.

## Certificates

OpenSSL helper scripts are provided in `cert-generation-examples/`.

Create a client CA and client certificate:

```sh
./cert-generation-examples/create-client-cert.sh client-certs systemd-service-toggle-client
```

Create a server CA and server certificate for development:

```sh
./cert-generation-examples/create-server-cert.sh server-certs vpn.example.org
```

For production servers, a public CA certificate such as a certbot certificate is
usually preferable for the server certificate. The client certificate should
still be issued by your private client CA.

## Build

Build the client:

```sh
go build -o systemd-service-toggle ./systemd-service-toggle
```

Build the server:

```sh
go build -o systemd-service-toggled ./systemd-service-toggled
```

Cross-compile the client for Windows:

```sh
GOOS=windows GOARCH=amd64 go build -o systemd-service-toggle.exe ./systemd-service-toggle
```

## systemd

An example unit file is provided:

```text
systemd-service-toggled.service
```

Install it according to your distribution's systemd conventions and adjust
paths if needed.

## License

MIT

Copyright (c) 2026 Thomas Kuhlmann
