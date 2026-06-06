// Package main implements the systemd-service-toggled TLS server.
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
	"time"

	"golang.org/x/crypto/argon2"
	"golang.org/x/term"
	"gopkg.in/yaml.v3"

	"systemd-service-toggle/common"
)

const (
	defaultListen = "0.0.0.0"
	selfService   = "systemd-service-toggled.service"

	wrongPasswordLimit = 10
	wrongPasswordDelay = 3 * time.Minute
)

type secretFile struct {
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
	passwd := common.HasArg(os.Args[1:], "--passwd")
	setupLog(dev || passwd)

	configPath, configDir := common.FindConfig("config-server.yml")
	fixServerPerms(configDir, configPath)
	loaded := common.LoadConfigPath(configPath)

	if passwd {
		writeSecret(filepath.Join(loaded.Dir, "secret"))
		return
	}

	cfg := loaded.Config
	if cfg.Server.Listen == "" {
		cfg.Server.Listen = defaultListen
	}

	cert, err := tls.LoadX509KeyPair(cfg.TLS.Cert, cfg.TLS.Key)
	if err != nil {
		panic(err)
	}
	clientCAs := x509.NewCertPool()
	clientCA, err := os.ReadFile(cfg.TLS.ClientCACert)
	if err != nil {
		panic(err)
	}
	if !clientCAs.AppendCertsFromPEM(clientCA) {
		panic("failed to load client-ca-cert")
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
	syslogOut, err = syslog.New(syslog.LOG_DAEMON|syslog.LOG_INFO, "systemd-service-toggled")
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

func fixServerPerms(dir, configPath string) {
	chmodIfNeeded(dir, 0700)
	chmodIfNeeded(configPath, 0600)

	secretPath := filepath.Join(dir, "secret")
	if _, err := os.Stat(secretPath); err == nil {
		chmodIfNeeded(secretPath, 0600)
	}
}

func chmodIfNeeded(path string, mode os.FileMode) {
	info, err := os.Stat(path)
	if err != nil {
		panic(err)
	}
	if info.Mode().Perm() == mode {
		return
	}
	if err := os.Chmod(path, mode); err != nil {
		panic(err)
	}
	warnf("fixed permissions on %s to %04o", path, mode)
}

func writeSecret(path string) {
	fmt.Print("Password: ")
	pass, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println()
	if err != nil {
		panic(err)
	}
	defer common.Wipe(pass)

	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		panic(err)
	}

	secret := secretFile{
		Salt:    base64.StdEncoding.EncodeToString(salt),
		Time:    5,
		Memory:  64 * 1024,
		Threads: 1,
		KeyLen:  32,
	}
	hash := argon2.IDKey(pass, salt, secret.Time, secret.Memory, secret.Threads, secret.KeyLen)
	defer common.Wipe(hash)
	secret.Hash = base64.StdEncoding.EncodeToString(hash)

	data, err := yaml.Marshal(secret)
	if err != nil {
		panic(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		panic(err)
	}
	if err := os.Chmod(path, 0600); err != nil {
		panic(err)
	}
	logger.Printf("wrote %s", path)
}

func handleConn(conn net.Conn, configDir string, cfg common.Config, dev bool) {
	defer conn.Close() //nolint:errcheck // Connection is closed after one request; close errors are not actionable.

	if err := conn.SetDeadline(time.Now().Add(time.Duration(cfg.Server.Timeout) * time.Second)); err != nil {
		logger.Printf("deadline failed: %v", err)
		return
	}

	if !checkClientCN(conn, cfg.TLS.ClientCN) {
		return
	}

	pass, err := common.ReadPassword(conn)
	if err != nil {
		logger.Printf("read failed: %v", err)
		return
	}
	defer common.Wipe(pass)

	ok := checkPassword(filepath.Join(configDir, "secret"), pass)
	if !ok {
		wrongPassword()
		return
	}

	wrongPasses = 0
	toggleService(cfg, dev)
}

func checkClientCN(conn net.Conn, want string) bool {
	tlsConn, ok := conn.(*tls.Conn)
	if !ok {
		panic("expected tls connection")
	}
	if err := tlsConn.Handshake(); err != nil {
		logger.Printf("tls handshake failed: %v", err)
		return false
	}
	if want == "" {
		return true
	}

	certs := tlsConn.ConnectionState().PeerCertificates
	if len(certs) == 0 {
		logger.Print("client certificate missing")
		return false
	}
	if certs[0].Subject.CommonName != want {
		logger.Printf("client certificate CN mismatch: %q", certs[0].Subject.CommonName)
		return false
	}
	return true
}

func wrongPassword() {
	wrongPasses++

	logger.Printf("wrong password attempt %d", wrongPasses)
	if wrongPasses >= wrongPasswordLimit {
		logger.Print("wrong password limit reached, disabling service", "limit", wrongPasswordLimit)
		runSystemctl("disable", selfService)
		runSystemctl("stop", selfService)
		return
	}

	delay := time.Duration(wrongPasses*wrongPasses) * wrongPasswordDelay
	logger.Printf("waiting after wrong password: %s", delay)
	time.Sleep(delay)
}

func checkPassword(path string, pass []byte) bool {
	data, err := os.ReadFile(path) // Read stored Argon2id parameters and hash.
	if err != nil {
		panic(err)
	}

	var secret secretFile
	if err := yaml.Unmarshal(data, &secret); err != nil { // Secret is YAML for readability.
		panic(err)
	}

	salt, err := base64.StdEncoding.DecodeString(secret.Salt) // Argon2Id Salt is stored as base64 text.
	if err != nil {
		panic(err)
	}
	want, err := base64.StdEncoding.DecodeString(secret.Hash) // Argon2id hash is stored as base64 text.
	if err != nil {
		panic(err)
	}
	defer common.Wipe(salt)
	defer common.Wipe(want)

	got := argon2.IDKey(pass, salt, secret.Time, secret.Memory, secret.Threads, secret.KeyLen) // Recompute with stored parameters.
	defer common.Wipe(got)

	return len(got) == len(want) && subtle.ConstantTimeCompare(got, want) == 1 // Constant-time compare avoids leaking prefix matches.
}

func toggleService(cfg common.Config, dev bool) {
	service := cfg.Service.Name
	if dev {
		logger.Printf("dev mode: would toggle %s", service)
		return
	}

	cmd := exec.Command("systemctl", "is-active", "--quiet", service)
	if err := cmd.Run(); err == nil {
		runSystemctl("stop", service)
		logger.Printf("stopped %s", service)
		return
	}

	runSystemctl("start", service)
	logger.Printf("started %s", service)
}

func runSystemctl(action, service string) {
	cmd := exec.Command("systemctl", action, service)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		panic(fmt.Sprintf("systemctl %s %s failed: %v: %s", action, service, err, stderr.String()))
	}
}
