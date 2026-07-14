// Package main implements the remote-systemd-toggle TLS client.
package main

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"os"
	"time"

	"remote-systemd-toggle/common"

	"golang.org/x/term"
)

var version = "dev"

func password() []byte {
	for i, arg := range os.Args[1:] {
		if arg == "--password" {
			if i+2 >= len(os.Args) {
				panic("missing value for --password")
			}
			return []byte(os.Args[i+2])
		}
	}

	fmt.Print("Password: ")
	pass, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println()
	if err != nil {
		panic(err)
	}
	return pass
}

func command() byte {
	if common.HasArg(os.Args[1:], "--status") {
		panic("--status is no longer supported, use: status")
	}
	if len(os.Args) < 2 {
		return common.CmdToggle
	}
	switch os.Args[1] {
	case "status":
		return common.CmdStatus
	case "toggle":
		return common.CmdToggle
	default:
		return common.CmdToggle
	}
}

func main() {
	dev := common.HasArg(os.Args[1:], "--dev")
	if !dev {
		defer func() {
			if r := recover(); r != nil {
				fmt.Fprintf(os.Stderr, "panic: %v\n", r)
				os.Exit(2)
			}
		}()
	}
	if common.HasArg(os.Args[1:], "--version") {
		fmt.Println(version)
		return
	}
	cmd := command()

	loaded := common.LoadConfig("config-client.yml")
	cfg := loaded.Config

	certPool, err := x509.SystemCertPool()
	if err != nil {
		panic(err)
	}
	if cfg.TLS.ServerCACert != "" {
		data, err := os.ReadFile(cfg.TLS.ServerCACert)
		if err != nil {
			panic(fmt.Sprintf("failed to read TLS.server-ca-cert %q: %v", cfg.TLS.ServerCACert, err))
		}
		if !certPool.AppendCertsFromPEM(data) {
			panic(fmt.Sprintf("failed to load TLS.server-ca-cert %q", cfg.TLS.ServerCACert))
		}
	}

	cert, err := tls.LoadX509KeyPair(cfg.TLS.Cert, cfg.TLS.Key)
	if err != nil {
		panic(fmt.Sprintf("failed to load TLS.cert %q and TLS.key %q: %v", cfg.TLS.Cert, cfg.TLS.Key, err))
	}

	pass := password()
	defer common.Wipe(pass)

	dialer := &net.Dialer{Timeout: time.Duration(cfg.Server.Timeout) * time.Second}
	conn, err := tls.DialWithDialer(dialer, "tcp", fmt.Sprintf("%s:%d", cfg.Server.Address, cfg.Server.Port), &tls.Config{
		ServerName:   cfg.Server.Address,
		Certificates: []tls.Certificate{cert},
		RootCAs:      certPool, // System CAs, optionally extended with TLS.server-ca-cert.
		MinVersion:   tls.VersionTLS13,
	})
	if err != nil {
		panic(err)
	}
	defer conn.Close() //nolint:errcheck // Nothing useful can be done after sending the request.

	if err := conn.SetDeadline(time.Now().Add(time.Duration(cfg.Server.Timeout) * time.Second)); err != nil {
		panic(err)
	}
	if err := common.WriteRequest(conn, cmd, pass); err != nil {
		panic(err)
	}
	status, err := common.ReadStatus(conn)
	if err != nil {
		panic(err)
	}
	fmt.Println(common.StatusText(status))
}
