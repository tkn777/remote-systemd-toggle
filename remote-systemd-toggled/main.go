// Package main implements the remote-systemd-toggled TLS server.
package main

import (
	"bytes"
	"crypto/rand"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"fmt"
	"log"
	"log/syslog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/crypto/argon2"
	"golang.org/x/term"
	"gopkg.in/yaml.v3"

	"remote-systemd-toggle/common"
)

const (
	defaultListen = "0.0.0.0"
	selfService   = "remote-systemd-toggled.service"
)

type secretData struct {
	Salt    string `yaml:"salt"`
	Hash    string `yaml:"hash"`
	Time    uint32 `yaml:"time"`
	Memory  uint32 `yaml:"memory"`
	Threads uint8  `yaml:"threads"`
	KeyLen  uint32 `yaml:"key_len"`
}

var (
	logger      *log.Logger
	syslogOut   *syslog.Writer
	wrongPasses int
)

var version = "dev"

func main() {
	if common.HasArg(os.Args[1:], "--version") {
		fmt.Println(version)
		return
	}

	dev := common.HasArg(os.Args[1:], "--dev")
	if !dev {
		defer func() {
			if r := recover(); r != nil {
				if logger != nil {
					logger.Printf("panic: %v", r)
				} else {
					fmt.Fprintf(os.Stderr, "panic: %v\n", r)
				}
				os.Exit(2)
			}
		}()
	}
	passwd := common.HasArg(os.Args[1:], "--passwd")
	setupLog(dev || passwd)

	configPath, configDir := common.FindConfig("config-server.yml")
	loaded := common.LoadConfigPath(configPath)
	fixServerPerms(configDir, configPath, loaded.Config)

	if passwd {
		writeSecret(secretPath(loaded.Dir, loaded.Config), loaded.Config)
		return
	}

	cfg := loaded.Config
	if cfg.Server.Listen == "" {
		cfg.Server.Listen = defaultListen
	}

	cert, err := tls.LoadX509KeyPair(cfg.TLS.Cert, cfg.TLS.Key)
	if err != nil {
		panic(fmt.Sprintf("failed to load TLS.cert %q and TLS.key %q: %v", cfg.TLS.Cert, cfg.TLS.Key, err))
	}
	clientCAs := x509.NewCertPool()
	clientCA, err := os.ReadFile(cfg.TLS.ClientCACert)
	if err != nil {
		panic(fmt.Sprintf("failed to read TLS.client-ca-cert %q: %v", cfg.TLS.ClientCACert, err))
	}
	if !clientCAs.AppendCertsFromPEM(clientCA) {
		panic(fmt.Sprintf("failed to load TLS.client-ca-cert %q", cfg.TLS.ClientCACert))
	}

	ln, err := tls.Listen("tcp", fmt.Sprintf("%s:%d", cfg.Server.Listen, cfg.Server.Port), &tls.Config{
		Certificates: []tls.Certificate{cert},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    clientCAs,
		MinVersion:   tls.VersionTLS13,
	})
	if err != nil {
		panic(err)
	}
	defer ln.Close() //nolint:errcheck // Listener is closed during shutdown; close errors are not actionable.

	logger.Printf("listening on %s:%d", cfg.Server.Listen, cfg.Server.Port)
	for {
		conn, err := ln.Accept()
		if err != nil {
			logger.Printf("accept failed: %v", err)
			continue
		}
		handleConn(conn, loaded.Dir, cfg, dev)
	}
}

func setupLog(stdout bool) {
	if stdout {
		logger = log.New(os.Stdout, "", log.LstdFlags)
		return
	}

	var err error
	syslogOut, err = syslog.New(syslog.LOG_DAEMON|syslog.LOG_INFO, "remote-systemd-toggled")
	if err != nil {
		panic(err)
	}
	logger = log.New(syslogOut, "", 0)
}

func warnf(format string, v ...any) {
	msg := fmt.Sprintf("warning: "+format, v...)
	if syslogOut != nil {
		_ = syslogOut.Warning(msg)
		return
	}
	logger.Print(msg)
}

func fixServerPerms(dir, configPath string, cfg common.Config) {
	chmodIfNeeded(dir, 0700)
	chmodIfNeeded(configPath, 0600)

	path := secretPath(dir, cfg)
	if _, err := os.Stat(path); err == nil {
		chmodIfNeeded(path, 0600)
	}
}

func chmodIfNeeded(path string, mode os.FileMode) {
	info, err := os.Stat(path)
	if err != nil {
		panic(fmt.Sprintf("failed to stat %s for permission fix: %v", path, err))
	}
	if info.Mode().Perm() == mode {
		return
	}
	if err := os.Chmod(path, mode); err != nil {
		panic(fmt.Sprintf("failed to chmod %s to %04o: %v", path, mode, err))
	}
	warnf("fixed permissions on %s to %04o", path, mode)
}

func writeSecret(path string, cfg common.Config) {
	fmt.Print("Password: ")
	pass, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println()
	if err != nil {
		panic(err)
	}
	defer common.Wipe(pass)

	secret := newSecret(pass, cfg)

	data, err := yaml.Marshal(secret)
	if err != nil {
		panic(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		panic(fmt.Sprintf("failed to write secrets file %s: %v", path, err))
	}
	if err := os.Chmod(path, 0600); err != nil {
		panic(fmt.Sprintf("failed to chmod secrets file %s to 0600: %v", path, err))
	}
	logger.Printf("wrote %s", path)
}

func newSecret(pass []byte, cfg common.Config) secretData {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		panic(err)
	}
	defer common.Wipe(salt)

	secret := secretData{
		Salt:    base64.StdEncoding.EncodeToString(salt),
		Time:    cfg.Secrets.Argon2Time,
		Memory:  cfg.Secrets.Argon2Memory,
		Threads: cfg.Secrets.Argon2Threads,
		KeyLen:  cfg.Secrets.Argon2KeyLen,
	}
	hash := argon2.IDKey(pass, salt, secret.Time, secret.Memory, secret.Threads, secret.KeyLen)
	defer common.Wipe(hash)
	secret.Hash = base64.StdEncoding.EncodeToString(hash)
	return secret
}

func handleConn(conn net.Conn, configDir string, cfg common.Config, dev bool) {
	defer conn.Close() //nolint:errcheck // Connection is closed after one request; close errors are not actionable.
	remote := remoteHost(conn)

	if err := conn.SetDeadline(time.Now().Add(time.Duration(cfg.Server.Timeout) * time.Second)); err != nil {
		logger.Printf("deadline failed from %s: %v", remote, err)
		return
	}

	if !checkClientCN(conn, cfg.TLS.ClientCN, remote) {
		if err := common.WriteStatus(conn, common.StatusUnauthorized); err != nil {
			logger.Printf("status write failed: %v", err)
		}
		return
	}

	cmd, pass, err := common.ReadRequest(conn)
	if err != nil {
		logger.Printf("read failed from %s: %v", remote, err)
		return
	}
	defer common.Wipe(pass)

	ok := checkPassword(secretPath(configDir, cfg), pass)
	if !ok {
		if err := common.WriteStatus(conn, common.StatusUnauthorized); err != nil {
			logger.Printf("status write failed: %v", err)
		}
		wrongPassword(cfg, dev, remote)
		return
	}

	wrongPasses = 0
	switch cmd {
	case common.CmdToggle:
		if err := common.WriteStatus(conn, toggleService(cfg, dev)); err != nil {
			logger.Printf("status write failed: %v", err)
		}
	case common.CmdStatus:
		if err := common.WriteStatus(conn, serviceStatus(cfg, dev)); err != nil {
			logger.Printf("status write failed: %v", err)
		}
	default:
		logger.Printf("unknown command: %d", cmd)
		if err := common.WriteStatus(conn, common.StatusUnknown); err != nil {
			logger.Printf("status write failed: %v", err)
		}
	}
}

func secretPath(configDir string, cfg common.Config) string {
	if cfg.Secrets.Path == "" {
		return filepath.Join(configDir, "secrets.yml")
	}
	if filepath.IsAbs(cfg.Secrets.Path) {
		return cfg.Secrets.Path
	}
	return filepath.Join(configDir, cfg.Secrets.Path)
}

func remoteHost(conn net.Conn) string {
	host, _, err := net.SplitHostPort(conn.RemoteAddr().String())
	if err != nil {
		return conn.RemoteAddr().String()
	}
	return host
}

func checkClientCN(conn net.Conn, want string, remote string) bool {
	tlsConn, ok := conn.(*tls.Conn)
	if !ok {
		panic("expected tls connection")
	}
	if err := tlsConn.Handshake(); err != nil {
		logger.Printf("tls handshake failed from %s: %v", remote, err)
		return false
	}
	if want == "" {
		return true
	}

	certs := tlsConn.ConnectionState().PeerCertificates
	if len(certs) == 0 {
		logger.Printf("client certificate missing from %s", remote)
		return false
	}
	if certs[0].Subject.CommonName != want {
		logger.Printf("client certificate CN mismatch from %s: expected %q, got %q", remote, want, certs[0].Subject.CommonName)
		return false
	}
	return true
}

func wrongPassword(cfg common.Config, dev bool, remote string) {
	wrongPasses++

	logger.Printf("wrong password attempt %d from %s", wrongPasses, remote)
	if wrongPasses >= cfg.Server.WrongPasswordLimit {
		if dev {
			logger.Printf("wrong password limit reached, would disable and stop %s", selfService)
			os.Exit(1)
		}
		logger.Print("wrong password limit reached, disabling service", "limit", cfg.Server.WrongPasswordLimit)
		runSystemctl("disable", selfService)
		runSystemctl("stop", selfService)
		return
	}

	delay := time.Duration(wrongPasses*wrongPasses) * time.Duration(cfg.Server.WrongPasswordDelayMinutes) * time.Minute
	if dev {
		logger.Printf("would wait after wrong password: %s", delay)
		return
	}
	logger.Printf("waiting after wrong password: %s", delay)
	time.Sleep(delay)
}

func checkPassword(path string, pass []byte) bool {
	data, err := os.ReadFile(path) // Read stored Argon2id parameters and hash.
	if err != nil {
		panic(fmt.Sprintf("failed to read secrets file %s: %v", path, err))
	}

	var secret secretData
	if err := yaml.Unmarshal(data, &secret); err != nil { // Secret is YAML for readability.
		panic(fmt.Sprintf("failed to parse secrets file %s: %v", path, err))
	}

	salt, err := base64.StdEncoding.DecodeString(secret.Salt) // Argon2Id Salt is stored as base64 text.
	if err != nil {
		panic(fmt.Sprintf("failed to decode salt in secrets file %s: %v", path, err))
	}
	want, err := base64.StdEncoding.DecodeString(secret.Hash) // Argon2id hash is stored as base64 text.
	if err != nil {
		panic(fmt.Sprintf("failed to decode hash in secrets file %s: %v", path, err))
	}
	defer common.Wipe(salt)
	defer common.Wipe(want)

	got := argon2.IDKey(pass, salt, secret.Time, secret.Memory, secret.Threads, secret.KeyLen) // Recompute with stored parameters.
	defer common.Wipe(got)

	return len(got) == len(want) && subtle.ConstantTimeCompare(got, want) == 1 // Constant-time compare avoids leaking prefix matches.
}

func toggleService(cfg common.Config, dev bool) byte {
	service := cfg.Service.Name
	if dev {
		logger.Printf("dev mode: would toggle %s", service)
		return common.StatusUnknown
	}

	if serviceStatus(cfg, false) == common.StatusActive {
		runSystemctl("stop", service)
		logger.Printf("stopped %s", service)
		return serviceStatus(cfg, false)
	}

	runSystemctl("start", service)
	logger.Printf("started %s", service)
	return serviceStatus(cfg, false)
}

func serviceStatus(cfg common.Config, dev bool) byte {
	service := cfg.Service.Name
	if dev {
		logger.Printf("dev mode: would read status of %s", service)
		return common.StatusUnknown
	}

	out, err := exec.Command("systemctl", "is-active", service).Output()
	switch strings.TrimSpace(string(out)) {
	case "active":
		return common.StatusActive
	case "inactive":
		return common.StatusInactive
	case "failed":
		return common.StatusFailed
	default:
		if err != nil {
			logger.Printf("status read failed: %v", err)
		}
		return common.StatusUnknown
	}
}

func runSystemctl(action, service string) {
	cmd := exec.Command("systemctl", action, service)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		panic(fmt.Sprintf("systemctl %s %s failed: %v: %s", action, service, err, stderr.String()))
	}
}
