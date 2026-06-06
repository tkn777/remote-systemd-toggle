// Package common contains shared config and wire protocol helpers for systemd-service-toggle.
package common

import (
	"encoding/binary"
	"fmt"
	"io"
)

func ReadPassword(r io.Reader) ([]byte, error) {
	var lenbuf [2]byte
	if _, err := io.ReadFull(r, lenbuf[:]); err != nil {
		return nil, err
	}

	pass := make([]byte, binary.BigEndian.Uint16(lenbuf[:]))
	if _, err := io.ReadFull(r, pass); err != nil {
		Wipe(pass)
		return nil, err
	}

	return pass, nil
}

func WritePassword(w io.Writer, pass []byte) error {
	if len(pass) > 65535 {
		return fmt.Errorf("password too long")
	}

	var lenbuf [2]byte
	binary.BigEndian.PutUint16(lenbuf[:], uint16(len(pass)))

	if err := writeAll(w, lenbuf[:]); err != nil {
		return err
	}
	return writeAll(w, pass)
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
