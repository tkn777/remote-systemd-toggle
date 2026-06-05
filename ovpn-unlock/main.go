// Package main implements the ovpn-unlock TLS client.
package main

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"os"
	"time"

	"ovpn-unlock/common"

	"golang.org/x/term"
)

func main() {
	loaded := common.LoadConfig("config-client.yml")
	cfg := loaded.Config

	certPool, err := x509.SystemCertPool()
	if err != nil {
		panic(err)
	}
	if cfg.TLS.ServerCACert != "" {
		data, err := os.ReadFile(cfg.TLS.ServerCACert)
		if err != nil {
			panic(err)
		}
		if !certPool.AppendCertsFromPEM(data) {
			panic("failed to load server-ca-cert")
		}
	}

	cert, err := tls.LoadX509KeyPair(cfg.TLS.Cert, cfg.TLS.Key)
	if err != nil {
		panic(err)
	}

	fmt.Print("Password: ")
	pass, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println()
	if err != nil {
		panic(err)
	}
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
	defer conn.Close()

	if err := conn.SetDeadline(time.Now().Add(time.Duration(cfg.Server.Timeout) * time.Second)); err != nil {
		panic(err)
	}
	if err := common.WritePassword(conn, pass); err != nil {
		panic(err)
	}
}
