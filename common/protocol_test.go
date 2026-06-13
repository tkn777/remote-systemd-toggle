package common

import (
	"bytes"
	"errors"
	"io"
	"testing"
)

func TestRequestRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	pass := []byte("secret")

	if err := WriteRequest(&buf, CmdStatus, pass); err != nil {
		t.Fatal(err)
	}

	cmd, got, err := ReadRequest(&buf)
	if err != nil {
		t.Fatal(err)
	}
	defer Wipe(got)

	if cmd != CmdStatus {
		t.Fatalf("cmd = %d, want %d", cmd, CmdStatus)
	}
	if string(got) != string(pass) {
		t.Fatalf("password = %q, want %q", got, pass)
	}
}

func TestWriteRequestRejectsTooLongPassword(t *testing.T) {
	err := WriteRequest(io.Discard, CmdToggle, make([]byte, 65536))
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestReadRequestRequiresFullPayload(t *testing.T) {
	_, _, err := ReadRequest(bytes.NewBuffer([]byte{CmdToggle, 0, 5, 's', 'e'}))
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("err = %v, want %v", err, io.ErrUnexpectedEOF)
	}
}

func TestStatusRoundTrip(t *testing.T) {
	var buf bytes.Buffer

	if err := WriteStatus(&buf, StatusActive); err != nil {
		t.Fatal(err)
	}

	status, err := ReadStatus(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if status != StatusActive {
		t.Fatalf("status = %d, want %d", status, StatusActive)
	}
}

func TestStatusText(t *testing.T) {
	tests := map[byte]string{
		StatusInactive:     "inactive",
		StatusActive:       "active",
		StatusFailed:       "failed",
		StatusUnknown:      "unknown",
		StatusUnauthorized: "unauthorized",
		99:                 "unknown",
	}

	for status, want := range tests {
		if got := StatusText(status); got != want {
			t.Fatalf("StatusText(%d) = %q, want %q", status, got, want)
		}
	}
}
