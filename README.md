# 🔐 remote-systemd-toggle

<p align="left">
  <img src="resources/project-logo.png" alt="remote-systemd-toggle" height=200>
</p>

---

## ⚠️ Experimental Software

**This project is highly experimental and under active development.**

**It is not production-ready and should not be used in any production environment under any circumstances.**

**Expect bugs, incomplete features, configuration changes, data corruption, data loss, and incompatible updates.**

**Use at your own risk. 🚧🔥**

---

## ℹ️ Description

`remote-systemd-toggle` is a small Go client/server tool for toggling a
configured systemd service remotely.

It uses TLS 1.3 with mutual TLS, an additional password check, and Argon2id for
password storage. The server is intended to run as root because it calls
`systemctl` directly.

---

## 🧩 Components

- `remote-systemd-toggled`: TLS server
- `remote-systemd-toggle`: TLS client
- `common`: shared config and wire protocol code

---

## 🚀 Usage

1) `remote-systemd-toggle` toggles the configured service.
2) `remote-systemd-toggle --status` prints the current status: `active`, `inactive`, `failed`, or `unknown`.
3) The client prompts for a password and sends one authenticated request to the server.\
   For scripts, the client also accepts `--password <password>` and skips the prompt.
4) The server accepts one connection at a time, reads one request, verifies the password, and then executes the requested command.

---

## 🛡️ Security Model

The server is designed to be reachable over an untrusted network, but only with
strict authentication:

- TLS 1.3 only
- mutual TLS is required
- the server verifies the client certificate against `TLS.client-ca-cert`
- the server can additionally verify the client certificate CN with `TLS.client-cn`
- the client verifies the server certificate using system CAs plus optional `TLS.server-ca-cert`
- toggle and status requests both require mTLS and the password
- passwords are read through a hidden prompt unless `--password` is used for scripts
- passwords are never logged
- password bytes are wiped after use where practical
- the password hash is stored as Argon2id parameters plus salt/hash in YAML
- the `secret` file is written with `0600`
- the server config directory is corrected to `0700`
- the server config and `secret` file are corrected to `0600`

After wrong passwords, the server waits increasingly longer:

```go
delay = wrong_attempts * wrong_attempts * 3 minutes // 3 can be changed in config
```

On the tenth *(can be changed in config)* wrong password, the server disables and stops itself with `systemctl`. (In `--dev` mode it only logs what it would do, does not wait after wrong passwords, and exits at the limit.)

---

## 📦 Release Artifacts

GitHub releases provide Debian packages, Red Hat compatible RPM packages, and a Windows client binary.

- The Linux packages are built for `amd64` and `arm64`. 
- The Windows artifact contains the client only.

If you need support for other architectures, just open an issue.

#### 🗄️ Debian Repository

You can use the Debian repository provided by `thk-systems.net` to receive automatic updates (currently only amd64):

```bash
curl -fsSL https://debian.thk-systems.net/repo-install.sh | sudo sh
sudo apt install remote-systemd-toggle-server  (or/and)
sudo apt install remote-systemd-toggle-client
```

---

## 🔨 Build

First get source code tarball from a release (or clone the repository, but this is not recommended, because it is under development).

#### Build the client:

```sh
go build -o remote-systemd-toggle ./remote-systemd-toggle
```

#### Build the server:

```sh
go build -o remote-systemd-toggled ./remote-systemd-toggled
```

#### Cross-compile the client for Windows:

```sh
GOOS=windows GOARCH=amd64 go build -o remote-systemd-toggle.exe ./remote-systemd-toggle
```

---

## 🏷️ Version

Both binaries support `--version`:

```sh
remote-systemd-toggle --version
remote-systemd-toggled --version
```

---

## ⚙️ Configuration

The client searches:

```text
~/.config/remote-systemd-toggle/config-client.yml
~/.remote-systemd-toggle/config-client.yml
/etc/remote-systemd-toggle/config-client.yml
```

The server searches:

```text
~/.config/remote-systemd-toggle/config-server.yml
~/.remote-systemd-toggle/config-server.yml
/etc/remote-systemd-toggle/config-server.yml
```

If you are using Windows, you should create a `.config` or a `.remote-systemd-toggle` directory in your user`s home directory.

Example configs are in `config-examples/`.

### 💻 Client Config

```yaml
Server:
  address: vpn.example.org
  port: 47112   # optional, default 47112
  timeout: 5    # optional, default 5 seconds

TLS:
  cert: /home/<user>/.config/remote-systemd-toggle/client.crt
  key: /home/<user>/.config/remote-systemd-toggle/client.key
  server-ca-cert: /home/<user>/.config/remote-systemd-toggle/server-ca.crt   # optional, extends system CAs
```

(see below how to create certificates)

### 🖥️ Server Config

```yaml
Server:
  listen: 0.0.0.0                   # optional, default 0.0.0.0
  port: 47112                       # optional, default 47112
  timeout: 5                        # optional, default 5 seconds
  wrong-password-limit: 10          # optional, default 10
  wrong-password-delay-minutes: 3   # optional, default 3

TLS:
  cert: /etc/letsencrypt/live/vpn.example.org/fullchain.pem
  key: /etc/letsencrypt/live/vpn.example.org/privkey.pem
  client-ca-cert: /etc/remote-systemd-toggle/client-ca.crt
  client-cn: remote-systemd-toggle-client   # optional, verifies the client certificate CN when set

Service:
  name: example.service
```

---

## 🔑 Password Setup

Create or replace the server-side password hash:

```sh
remote-systemd-toggled --passwd
```

This command prompts for a password, reads the server config, writes `secret` next to it, and exits.

---

## 📜 Certificates

OpenSSL helper scripts are provided in `cert-generation-examples/`.

#### Create a client CA and client certificate:

```sh
./cert-generation-examples/create-client-cert.sh client-certs remote-systemd-toggle-client
```

#### Create a server CA and server certificate for development:

```sh
./cert-generation-examples/create-server-cert.sh server-certs vpn.example.org
```

For production servers, a public CA certificate such as a certbot certificate is
usually preferable for the server certificate. The client certificate should
still be issued by your private client CA.

---

## 🧰 systemd integration

An example unit file is provided:

```text
remote-systemd-toggled.service
```

Install it according to your distribution's systemd conventions and adjust
paths if needed.

---

## 🧪 Development Mode

Run the server in development mode:

```sh
remote-systemd-toggled --dev
```

Development mode is completely non-destructive:

- Logs are written to stdout.
- The configured service is never started or stopped. The server only logs what it would do.
- Status requests return `unknown`; the server only logs that it would read the service status.
- No delay is applied after a wrong password. The calculated delay is logged, but execution continues immediately.
- No `systemctl` actions are executed after a last wrong password. The server only logs whether it would stop and exits.
- `remote-systemd-toggle --status` always returns `unknown` and does not check the service status.
- Stacktraces are printed.

---

## 🐕 Dedicated to Jessie

This project is dedicated to Jessie, my best friend ever.

He never left my side. Even when he was old and sick, he would fight his way up the stairs just to find me and be near me. We played together in the snow like two children, chasing sticks and sharing moments of pure joy.

Through good days and hard days, he was always there — loyal, gentle, and steadfast. His companionship, trust, and unconditional friendship shaped my life in ways words can hardly express.

Though he is gone, his paw prints remain on my heart, and the memories of our time together continue to bring both a smile and a tear.

You were not just a dog. You were family, my companion, and my friend. You will never be forgotten. 🐾

---

## 📄 License

MIT License

Copyright (c) 2026 Thomas Kuhlmann
