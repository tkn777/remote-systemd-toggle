package common

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfigDefaultsAndSearchOrder(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	primary := filepath.Join(home, ".config", "remote-systemd-toggle")
	fallback := filepath.Join(home, ".remote-systemd-toggle")
	if err := os.MkdirAll(primary, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(fallback, 0700); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(primary, "config-client.yml"), []byte("Server:\n  address: primary\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fallback, "config-client.yml"), []byte("Server:\n  address: fallback\n  port: 1234\n"), 0600); err != nil {
		t.Fatal(err)
	}

	loaded := LoadConfig("config-client.yml")

	if loaded.Config.Server.Address != "primary" {
		t.Fatalf("address = %q, want primary", loaded.Config.Server.Address)
	}
	if loaded.Config.Server.Port != 47112 {
		t.Fatalf("port = %d, want 47112", loaded.Config.Server.Port)
	}
	if loaded.Config.Server.Timeout != 5 {
		t.Fatalf("timeout = %d, want 5", loaded.Config.Server.Timeout)
	}
	if loaded.Config.Server.WrongPasswordLimit != 10 {
		t.Fatalf("wrong password limit = %d, want 10", loaded.Config.Server.WrongPasswordLimit)
	}
	if loaded.Config.Server.WrongPasswordDelayMinutes != 3 {
		t.Fatalf("wrong password delay = %d, want 3", loaded.Config.Server.WrongPasswordDelayMinutes)
	}
	if loaded.Dir != primary {
		t.Fatalf("dir = %q, want %q", loaded.Dir, primary)
	}
}

func TestFindConfigUsesDotDirFallback(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	dir := filepath.Join(home, ".remote-systemd-toggle")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "config-server.yml")
	if err := os.WriteFile(path, []byte("Server:\n  port: 1234\n"), 0600); err != nil {
		t.Fatal(err)
	}

	gotPath, gotDir := FindConfig("config-server.yml")
	if gotPath != path {
		t.Fatalf("path = %q, want %q", gotPath, path)
	}
	if gotDir != dir {
		t.Fatalf("dir = %q, want %q", gotDir, dir)
	}
}
