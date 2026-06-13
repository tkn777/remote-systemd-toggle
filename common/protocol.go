// Package common contains shared config and wire protocol helpers for remote-systemd-toggle.
package common

import (
	"encoding/binary"
	"fmt"
	"io"
)

const (
	CmdToggle byte = 1
	CmdStatus byte = 2

	StatusInactive     byte = 0
	StatusActive       byte = 1
	StatusFailed       byte = 2
	StatusUnknown      byte = 3
	StatusUnauthorized byte = 4
)

func ReadRequest(r io.Reader) (byte, []byte, error) {
	var hdr [3]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return 0, nil, err
	}

	pass := make([]byte, binary.BigEndian.Uint16(hdr[1:]))
	if _, err := io.ReadFull(r, pass); err != nil {
		Wipe(pass)
		return 0, nil, err
	}

	return hdr[0], pass, nil
}

func WriteRequest(w io.Writer, cmd byte, pass []byte) error {
	if len(pass) > 65535 {
		return fmt.Errorf("password too long")
	}

	var hdr [3]byte
	hdr[0] = cmd
	binary.BigEndian.PutUint16(hdr[1:], uint16(len(pass)))

	if err := writeAll(w, hdr[:]); err != nil {
		return err
	}
	return writeAll(w, pass)
}

func ReadStatus(r io.Reader) (byte, error) {
	var b [1]byte
	_, err := io.ReadFull(r, b[:])
	return b[0], err
}

func WriteStatus(w io.Writer, status byte) error {
	return writeAll(w, []byte{status})
}

func StatusText(status byte) string {
	switch status {
	case StatusInactive:
		return "inactive"
	case StatusActive:
		return "active"
	case StatusFailed:
		return "failed"
	case StatusUnauthorized:
		return "unauthorized"
	default:
		return "unknown"
	}
}

func Wipe(b []byte) {
	for i := range b {
		b[i] = 0
	}
}

func writeAll(w io.Writer, b []byte) error {
	for len(b) > 0 {
		n, err := w.Write(b)
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		b = b[n:]
	}
	return nil
}
