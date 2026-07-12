package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"io"
	"log"
	"math/big"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/argon2"
	"gopkg.in/yaml.v3"

	"remote-systemd-toggle/common"
)

func TestMTLSStatusDevMode(t *testing.T) {
	cfg, configDir, serverTLS, clientTLS := testSetup(t, "secret")
	ln := testListener(t, serverTLS)
	done := serveOnce(t, ln, configDir, cfg, true)

	conn := testDial(t, ln.Addr().String(), clientTLS)
	defer conn.Close() //nolint:errcheck // Test cleanup.

	if err := common.WriteRequest(conn, common.CmdStatus, []byte("secret")); err != nil {
		t.Fatal(err)
	}
	status, err := common.ReadStatus(conn)
	if err != nil {
		t.Fatal(err)
	}
	if status != common.StatusUnknown {
		t.Fatalf("status = %d, want %d", status, common.StatusUnknown)
	}

	<-done
}

func TestMTLSToggleDevMode(t *testing.T) {
	cfg, configDir, serverTLS, clientTLS := testSetup(t, "secret")
	ln := testListener(t, serverTLS)
	done := serveOnce(t, ln, configDir, cfg, true)

	conn := testDial(t, ln.Addr().String(), clientTLS)
	defer conn.Close() //nolint:errcheck // Test cleanup.

	if err := common.WriteRequest(conn, common.CmdToggle, []byte("secret")); err != nil {
		t.Fatal(err)
	}
	status, err := common.ReadStatus(conn)
	if err != nil {
		t.Fatal(err)
	}
	if status != common.StatusUnknown {
		t.Fatalf("status = %d, want %d", status, common.StatusUnknown)
	}

	<-done
}

func TestMTLSRejectsMissingClientCertificate(t *testing.T) {
	cfg, configDir, serverTLS, clientTLS := testSetup(t, "secret")
	clientTLS.Certificates = nil
	ln := testListener(t, serverTLS)
	done := serveOnce(t, ln, configDir, cfg, true)

	conn := testDial(t, ln.Addr().String(), clientTLS)
	defer conn.Close() //nolint:errcheck // Test cleanup.

	err := common.WriteRequest(conn, common.CmdStatus, []byte("secret"))
	if err == nil {
		_, err = common.ReadStatus(conn)
	}
	if err == nil {
		t.Fatal("expected mTLS failure")
	}

	<-done
}

func TestMTLSRejectsClientCNMismatch(t *testing.T) {
	cfg, configDir, serverTLS, clientTLS := testSetup(t, "secret")
	cfg.TLS.ClientCN = "other-client"
	ln := testListener(t, serverTLS)
	done := serveOnce(t, ln, configDir, cfg, true)

	conn := testDial(t, ln.Addr().String(), clientTLS)
	defer conn.Close() //nolint:errcheck // Test cleanup.

	if err := common.WriteRequest(conn, common.CmdStatus, []byte("secret")); err != nil {
		t.Fatal(err)
	}
	status, err := common.ReadStatus(conn)
	if err != nil {
		t.Fatal(err)
	}
	if status != common.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", status, common.StatusUnauthorized)
	}

	<-done
}

func TestWrongPasswordDevMode(t *testing.T) {
	cfg, configDir, serverTLS, clientTLS := testSetup(t, "secret")
	ln := testListener(t, serverTLS)
	done := serveOnce(t, ln, configDir, cfg, true)

	conn := testDial(t, ln.Addr().String(), clientTLS)
	defer conn.Close() //nolint:errcheck // Test cleanup.

	if err := common.WriteRequest(conn, common.CmdToggle, []byte("wrong")); err != nil {
		t.Fatal(err)
	}
	status, err := common.ReadStatus(conn)
	if err != nil {
		t.Fatal(err)
	}
	if status != common.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", status, common.StatusUnauthorized)
	}

	<-done
	if wrongPasses != 1 {
		t.Fatalf("wrongPasses = %d, want 1", wrongPasses)
	}
}

func TestWrongPasswordLimitDevModeExits(t *testing.T) {
	if os.Getenv("REMOTE_SYSTEMD_TOGGLE_EXIT_TEST") == "1" {
		runWrongPasswordLimitExitTest(t)
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestWrongPasswordLimitDevModeExits")
	cmd.Env = append(os.Environ(), "REMOTE_SYSTEMD_TOGGLE_EXIT_TEST=1")
	err := cmd.Run()
	if exitErr, ok := err.(*exec.ExitError); ok && !exitErr.Success() {
		return
	}
	t.Fatalf("err = %v, want non-zero exit", err)
}

func TestServiceStatusWithFakeSystemctl(t *testing.T) {
	logPath := installFakeSystemctl(t, "active")
	cfg := testServiceConfig()

	if got := serviceStatus(cfg, false); got != common.StatusActive {
		t.Fatalf("active status = %d, want %d", got, common.StatusActive)
	}

	t.Setenv("SYSTEMCTL_STATUS", "inactive")
	if got := serviceStatus(cfg, false); got != common.StatusInactive {
		t.Fatalf("inactive status = %d, want %d", got, common.StatusInactive)
	}

	t.Setenv("SYSTEMCTL_STATUS", "failed")
	if got := serviceStatus(cfg, false); got != common.StatusFailed {
		t.Fatalf("failed status = %d, want %d", got, common.StatusFailed)
	}

	t.Setenv("SYSTEMCTL_STATUS", "unknown")
	if got := serviceStatus(cfg, false); got != common.StatusUnknown {
		t.Fatalf("unknown status = %d, want %d", got, common.StatusUnknown)
	}

	if log := readSystemctlLog(t, logPath); strings.Count(log, "is-active example.service\n") != 4 {
		t.Fatalf("log = %q", log)
	}
}

func TestToggleServiceWithFakeSystemctl(t *testing.T) {
	logPath := installFakeSystemctl(t, "active")
	if status := toggleService(testServiceConfig(), false); status != common.StatusActive {
		t.Fatalf("active toggle status = %d, want %d", status, common.StatusActive)
	}
	if log := readSystemctlLog(t, logPath); log != "is-active example.service\nstop example.service\nis-active example.service\n" {
		t.Fatalf("active toggle log = %q", log)
	}

	logPath = installFakeSystemctl(t, "inactive")
	if status := toggleService(testServiceConfig(), false); status != common.StatusInactive {
		t.Fatalf("inactive toggle status = %d, want %d", status, common.StatusInactive)
	}
	if log := readSystemctlLog(t, logPath); log != "is-active example.service\nstart example.service\nis-active example.service\n" {
		t.Fatalf("inactive toggle log = %q", log)
	}
}

func TestWrongPasswordLimitRunsSystemctl(t *testing.T) {
	logger = log.New(io.Discard, "", 0)
	wrongPasses = 1
	logPath := installFakeSystemctl(t, "inactive")

	wrongPassword(common.Config{
		Server: common.ServerConfig{
			WrongPasswordLimit:        2,
			WrongPasswordDelayMinutes: 3,
		},
	}, false, "127.0.0.1")

	if log := readSystemctlLog(t, logPath); log != "disable remote-systemd-toggled.service\nstop remote-systemd-toggled.service\n" {
		t.Fatalf("log = %q", log)
	}
}

func TestNewSecretAndCheckPassword(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secrets.yml")
	cfg := common.Config{
		Secrets: common.SecretsConfig{
			Argon2Time:    5,
			Argon2Memory:  64 * 1024,
			Argon2Threads: 1,
			Argon2KeyLen:  32,
		},
	}
	secret := newSecret([]byte("secret"), cfg)

	if secret.Time != 5 {
		t.Fatalf("time = %d, want 5", secret.Time)
	}
	if secret.Memory != 64*1024 {
		t.Fatalf("memory = %d, want %d", secret.Memory, 64*1024)
	}
	if secret.Threads != 1 {
		t.Fatalf("threads = %d, want 1", secret.Threads)
	}
	if secret.KeyLen != 32 {
		t.Fatalf("key len = %d, want 32", secret.KeyLen)
	}

	salt, err := base64.StdEncoding.DecodeString(secret.Salt)
	if err != nil {
		t.Fatal(err)
	}
	if len(salt) != 16 {
		t.Fatalf("salt length = %d, want 16", len(salt))
	}
	hash, err := base64.StdEncoding.DecodeString(secret.Hash)
	if err != nil {
		t.Fatal(err)
	}
	if len(hash) != int(secret.KeyLen) {
		t.Fatalf("hash length = %d, want %d", len(hash), secret.KeyLen)
	}

	data, err := yaml.Marshal(secret)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("mode = %04o, want 0600", info.Mode().Perm())
	}

	if !checkPassword(path, []byte("secret")) {
		t.Fatal("expected secret password to verify")
	}
	if checkPassword(path, []byte("wrong")) {
		t.Fatal("expected wrong password to fail")
	}
}

func TestNewSecretUsesConfiguredArgon2Params(t *testing.T) {
	secret := newSecret([]byte("secret"), common.Config{
		Secrets: common.SecretsConfig{
			Argon2Time:    2,
			Argon2Memory:  16 * 1024,
			Argon2Threads: 1,
			Argon2KeyLen:  16,
		},
	})

	if secret.Time != 2 {
		t.Fatalf("time = %d, want 2", secret.Time)
	}
	if secret.Memory != 16*1024 {
		t.Fatalf("memory = %d, want %d", secret.Memory, 16*1024)
	}
	if secret.Threads != 1 {
		t.Fatalf("threads = %d, want 1", secret.Threads)
	}
	if secret.KeyLen != 16 {
		t.Fatalf("key len = %d, want 16", secret.KeyLen)
	}
}

func TestSecretPath(t *testing.T) {
	configDir := t.TempDir()

	if got := secretPath(configDir, common.Config{}); got != filepath.Join(configDir, "secrets.yml") {
		t.Fatalf("default path = %q", got)
	}
	if got := secretPath(configDir, common.Config{Secrets: common.SecretsConfig{Path: "custom.yml"}}); got != filepath.Join(configDir, "custom.yml") {
		t.Fatalf("relative path = %q", got)
	}
	if got := secretPath(configDir, common.Config{Secrets: common.SecretsConfig{Path: "/tmp/custom.yml"}}); got != "/tmp/custom.yml" {
		t.Fatalf("absolute path = %q", got)
	}
}

func TestSecurePathFixesMode(t *testing.T) {
	logger = log.New(io.Discard, "", 0)
	path := filepath.Join(t.TempDir(), "config-server.yml")
	if err := os.WriteFile(path, []byte("Server:\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}

	securePath(path, 0600)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("mode = %04o, want 0600", info.Mode().Perm())
	}
}

func TestSecurePathRejectsSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	link := filepath.Join(dir, "link")
	if err := os.WriteFile(target, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	defer func() {
		if recover() == nil {
			t.Fatal("expected securePath to reject symlink")
		}
	}()
	securePath(link, 0600)
}

func TestSecureDirFixesMode(t *testing.T) {
	logger = log.New(io.Discard, "", 0)
	dir := filepath.Join(t.TempDir(), "secrets")
	if err := os.Mkdir(dir, 0755); err != nil {
		t.Fatal(err)
	}

	secureDir(dir)
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0700 {
		t.Fatalf("mode = %04o, want 0700", info.Mode().Perm())
	}
}

func TestCheckPasswordPanicsWhenSecretMissing(t *testing.T) {
	logger = log.New(io.Discard, "", 0)
	path := filepath.Join(t.TempDir(), "secrets.yml")

	defer func() {
		if recover() == nil {
			t.Fatal("expected missing secret to panic")
		}
	}()
	checkPassword(path, []byte("password"))
}

func runWrongPasswordLimitExitTest(t *testing.T) {
	logger = log.New(io.Discard, "", 0)
	wrongPasses = 0

	cfg, configDir, serverTLS, clientTLS := testSetup(t, "secret")
	cfg.Server.WrongPasswordLimit = 2

	ln1 := testListener(t, serverTLS)
	done := serveOnce(t, ln1, configDir, cfg, true)

	conn1 := testDial(t, ln1.Addr().String(), clientTLS)
	if err := common.WriteRequest(conn1, common.CmdToggle, []byte("wrong")); err != nil {
		t.Fatal(err)
	}
	if err := conn1.Close(); err != nil {
		t.Fatal(err)
	}
	<-done

	ln2 := testListener(t, serverTLS)
	serveOnce(t, ln2, configDir, cfg, true)

	conn2 := testDial(t, ln2.Addr().String(), clientTLS)
	if err := common.WriteRequest(conn2, common.CmdToggle, []byte("wrong")); err != nil {
		t.Fatal(err)
	}
	time.Sleep(500 * time.Millisecond)
	t.Fatal("expected os.Exit")
}

func testServiceConfig() common.Config {
	return common.Config{
		Service: common.ServiceConfig{
			Name: "example.service",
		},
	}
}

func installFakeSystemctl(t *testing.T, status string) string {
	t.Helper()

	dir := t.TempDir()
	logPath := filepath.Join(dir, "systemctl.log")
	script := filepath.Join(dir, "systemctl")
	data := `#!/bin/sh
printf '%s\n' "$*" >> "$SYSTEMCTL_LOG"
case "$1" in
	is-active)
		printf '%s\n' "$SYSTEMCTL_STATUS"
		[ "$SYSTEMCTL_STATUS" = active ]
		;;
	start|stop|disable)
		exit 0
		;;
	*)
		exit 1
		;;
esac
`
	if err := os.WriteFile(script, []byte(data), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("SYSTEMCTL_LOG", logPath)
	t.Setenv("SYSTEMCTL_STATUS", status)
	return logPath
}

func readSystemctlLog(t *testing.T, path string) string {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func testSetup(t *testing.T, pass string) (common.Config, string, *tls.Config, *tls.Config) {
	t.Helper()

	logger = log.New(io.Discard, "", 0)
	syslogOut = nil
	wrongPasses = 0

	dir := t.TempDir()
	writeTestSecret(t, filepath.Join(dir, "secrets.yml"), []byte(pass))

	caCert, caKey := testCA(t)
	serverCert := testCert(t, caCert, caKey, "localhost", true)
	clientCert := testCert(t, caCert, caKey, "test-client", false)

	pool := x509.NewCertPool()
	pool.AddCert(caCert)

	cfg := common.Config{
		Server: common.ServerConfig{
			Timeout:                   2,
			WrongPasswordLimit:        10,
			WrongPasswordDelayMinutes: 3,
		},
		TLS: common.TLSConfig{
			ClientCN: "test-client",
		},
		Service: common.ServiceConfig{
			Name: "dummy.service",
		},
	}

	serverTLS := &tls.Config{
		Certificates: []tls.Certificate{serverCert},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    pool,
		MinVersion:   tls.VersionTLS13,
	}
	clientTLS := &tls.Config{
		Certificates: []tls.Certificate{clientCert},
		RootCAs:      pool,
		ServerName:   "localhost",
		MinVersion:   tls.VersionTLS13,
	}

	return cfg, dir, serverTLS, clientTLS
}

func serveOnce(t *testing.T, ln net.Listener, configDir string, cfg common.Config, dev bool) <-chan struct{} {
	t.Helper()

	done := make(chan struct{})
	go func() {
		defer close(done)
		defer ln.Close() //nolint:errcheck // Test cleanup.

		conn, err := ln.Accept()
		if err != nil {
			t.Errorf("accept failed: %v", err)
			return
		}
		handleConn(conn, configDir, cfg, dev)
	}()
	return done
}

func testListener(t *testing.T, cfg *tls.Config) net.Listener {
	t.Helper()

	ln, err := tls.Listen("tcp", "127.0.0.1:0", cfg)
	if err != nil {
		t.Fatal(err)
	}
	return ln
}

func testDial(t *testing.T, addr string, cfg *tls.Config) net.Conn {
	t.Helper()

	dialer := &net.Dialer{Timeout: 2 * time.Second}
	conn, err := tls.DialWithDialer(dialer, "tcp", addr, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.SetDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatal(err)
	}
	return conn
}

func writeTestSecret(t *testing.T, path string, pass []byte) {
	t.Helper()

	salt := []byte("1234567890123456")
	secret := secretData{
		Salt:    base64.StdEncoding.EncodeToString(salt),
		Time:    1,
		Memory:  8 * 1024,
		Threads: 1,
		KeyLen:  32,
	}
	hash := argon2.IDKey(pass, salt, secret.Time, secret.Memory, secret.Threads, secret.KeyLen)
	secret.Hash = base64.StdEncoding.EncodeToString(hash)

	data, err := yaml.Marshal(secret)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
}

func testCA(t *testing.T) (*x509.Certificate, ed25519.PrivateKey) {
	t.Helper()

	pub, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "test CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, pub, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return cert, key
}

func testCert(t *testing.T, ca *x509.Certificate, caKey ed25519.PrivateKey, cn string, server bool) tls.Certificate {
	t.Helper()

	pub, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
	}
	if server {
		tmpl.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}
		tmpl.DNSNames = []string{"localhost"}
		tmpl.IPAddresses = []net.IP{net.ParseIP("127.0.0.1")}
	} else {
		tmpl.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}
	}

	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca, pub, caKey)
	if err != nil {
		t.Fatal(err)
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})

	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatal(err)
	}
	return cert
}
