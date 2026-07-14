package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"remote-systemd-toggle/common"
)

func TestClientToggle(t *testing.T) {
	ln, home := testClientSetup(t)
	done := testServeClientRequest(t, ln, common.CmdToggle, func(conn net.Conn) {
		if err := common.WriteStatus(conn, common.StatusActive); err != nil {
			t.Errorf("write status failed: %v", err)
		}
	})

	var out bytes.Buffer
	withClientStdout(t, &out, func() {
		withClientEnv(t, home, []string{"remote-systemd-toggle", "--password", "secret"}, func() {
			main()
		})
	})

	if got := out.String(); got != "active\n" {
		t.Fatalf("stdout = %q, want %q", got, "active\n")
	}

	<-done
}

func TestClientStatus(t *testing.T) {
	ln, home := testClientSetup(t)
	done := testServeClientRequest(t, ln, common.CmdStatus, func(conn net.Conn) {
		if err := common.WriteStatus(conn, common.StatusActive); err != nil {
			t.Errorf("write status failed: %v", err)
		}
	})

	var out bytes.Buffer
	withClientStdout(t, &out, func() {
		withClientEnv(t, home, []string{"remote-systemd-toggle", "status", "--password", "secret"}, func() {
			main()
		})
	})

	if got := out.String(); got != "active\n" {
		t.Fatalf("stdout = %q, want %q", got, "active\n")
	}

	<-done
}

func TestCommandDefaultsToToggleWithoutArgs(t *testing.T) {
	withClientArgs(t, []string{"remote-systemd-toggle"}, func() {
		if got := command(); got != common.CmdToggle {
			t.Fatalf("command = %d, want toggle", got)
		}
	})
}

func TestCommandDefaultsToToggleWithFlagFirst(t *testing.T) {
	withClientArgs(t, []string{"remote-systemd-toggle", "--dev"}, func() {
		if got := command(); got != common.CmdToggle {
			t.Fatalf("command = %d, want toggle", got)
		}
	})
}

func TestCommandDefaultsToToggleWhenStatusIsNotFirst(t *testing.T) {
	withClientArgs(t, []string{"remote-systemd-toggle", "--password", "secret", "status"}, func() {
		if got := command(); got != common.CmdToggle {
			t.Fatalf("command = %d, want toggle", got)
		}
	})
}

func TestCommandExplicitToggle(t *testing.T) {
	withClientArgs(t, []string{"remote-systemd-toggle", "toggle", "--password", "secret"}, func() {
		if got := command(); got != common.CmdToggle {
			t.Fatalf("command = %d, want toggle", got)
		}
	})
}

func TestCommandExplicitStatus(t *testing.T) {
	withClientArgs(t, []string{"remote-systemd-toggle", "status", "--password", "secret"}, func() {
		if got := command(); got != common.CmdStatus {
			t.Fatalf("command = %d, want status", got)
		}
	})
}

func TestCommandRejectsOldStatusFlag(t *testing.T) {
	withClientArgs(t, []string{"remote-systemd-toggle", "--status", "--password", "secret"}, func() {
		defer func() {
			if recover() == nil {
				t.Fatal("expected --status to panic")
			}
		}()
		command()
	})
}

func testClientSetup(t *testing.T) (net.Listener, string) {
	t.Helper()

	home := t.TempDir()
	configDir := filepath.Join(home, ".config", "remote-systemd-toggle")
	if err := os.MkdirAll(configDir, 0700); err != nil {
		t.Fatal(err)
	}

	caCert, caKey := testClientCA(t)
	serverCert := testClientCert(t, caCert, caKey, "localhost", true)
	clientCert := testClientCert(t, caCert, caKey, "test-client", false)

	caPath := filepath.Join(configDir, "server-ca.crt")
	clientCertPath := filepath.Join(configDir, "client.crt")
	clientKeyPath := filepath.Join(configDir, "client.key")

	writePEM(t, caPath, "CERTIFICATE", caCert.Raw, 0644)
	writeClientCertFiles(t, clientCertPath, clientKeyPath, clientCert)

	pool := x509.NewCertPool()
	pool.AddCert(caCert)
	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{
		Certificates: []tls.Certificate{serverCert},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    pool,
		MinVersion:   tls.VersionTLS13,
	})
	if err != nil {
		t.Fatal(err)
	}

	_, port, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	config := fmt.Sprintf(`Server:
  address: 127.0.0.1
  port: %s
  timeout: 2

TLS:
  cert: %s
  key: %s
  server-ca-cert: %s
`, port, clientCertPath, clientKeyPath, caPath)
	if err := os.WriteFile(filepath.Join(configDir, "config-client.yml"), []byte(config), 0600); err != nil {
		t.Fatal(err)
	}

	return ln, home
}

func testServeClientRequest(t *testing.T, ln net.Listener, wantCmd byte, reply func(net.Conn)) <-chan struct{} {
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
		defer conn.Close() //nolint:errcheck // Test cleanup.

		if err := conn.SetDeadline(time.Now().Add(2 * time.Second)); err != nil {
			t.Errorf("deadline failed: %v", err)
			return
		}

		cmd, pass, err := common.ReadRequest(conn)
		if err != nil {
			t.Errorf("read request failed: %v", err)
			return
		}
		defer common.Wipe(pass)

		if cmd != wantCmd {
			t.Errorf("cmd = %d, want %d", cmd, wantCmd)
		}
		if string(pass) != "secret" {
			t.Errorf("password = %q, want secret", pass)
		}
		if reply != nil {
			reply(conn)
		}
	}()
	return done
}

func withClientEnv(t *testing.T, home string, args []string, fn func()) {
	t.Helper()

	withClientArgs(t, args, func() {
		t.Setenv("HOME", home)
		fn()
	})
}

func withClientArgs(t *testing.T, args []string, fn func()) {
	t.Helper()

	oldArgs := os.Args
	os.Args = args
	defer func() {
		os.Args = oldArgs
	}()
	fn()
}

func withClientStdout(t *testing.T, out *bytes.Buffer, fn func()) {
	t.Helper()

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	defer func() {
		os.Stdout = oldStdout
	}()

	fn()

	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	os.Stdout = oldStdout
	if _, err := out.ReadFrom(r); err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
}

func testClientCA(t *testing.T) (*x509.Certificate, ed25519.PrivateKey) {
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

func testClientCert(t *testing.T, ca *x509.Certificate, caKey ed25519.PrivateKey, cn string, server bool) tls.Certificate {
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

func writeClientCertFiles(t *testing.T, certPath, keyPath string, cert tls.Certificate) {
	t.Helper()

	writePEM(t, certPath, "CERTIFICATE", cert.Certificate[0], 0644)
	key, ok := cert.PrivateKey.(ed25519.PrivateKey)
	if !ok {
		t.Fatal("expected ed25519 key")
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	writePEM(t, keyPath, "PRIVATE KEY", keyDER, 0600)
}

func writePEM(t *testing.T, path, typ string, der []byte, mode os.FileMode) {
	t.Helper()

	data := pem.EncodeToMemory(&pem.Block{Type: typ, Bytes: der})
	if err := os.WriteFile(path, data, mode); err != nil {
		t.Fatal(err)
	}
}
