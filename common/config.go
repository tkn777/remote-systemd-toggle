// Package common contains shared config and wire protocol helpers for ovpn-unlock.
package common

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server  ServerConfig  `yaml:"Server"`
	TLS     TLSConfig     `yaml:"TLS"`
	OpenVPN OpenVPNConfig `yaml:"OpenVPN"`
}

type ServerConfig struct {
	Listen  string `yaml:"listen"`
	Address string `yaml:"address"`
	Port    int    `yaml:"port"`
	Timeout int    `yaml:"timeout"`
}

type TLSConfig struct {
	Cert         string `yaml:"cert"`
	Key          string `yaml:"key"`
	ServerCACert string `yaml:"server-ca-cert"`
	ClientCACert string `yaml:"client-ca-cert"`
	ClientCN     string `yaml:"client-cn"`
}

type OpenVPNConfig struct {
	Service string `yaml:"service"`
}

type LoadedConfig struct {
	Config Config
	Path   string
	Dir    string
}

func LoadConfig() LoadedConfig {
	path, _ := FindConfig()
	return LoadConfigPath(path)
}

func LoadConfigPath(path string) LoadedConfig {
	data, err := os.ReadFile(path)
	if err != nil {
		panic(err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		panic(err)
	}

	if cfg.Server.Port == 0 {
		cfg.Server.Port = 47112
	}
	if cfg.Server.Timeout == 0 {
		cfg.Server.Timeout = 5
	}

	return LoadedConfig{
		Config: cfg,
		Path:   path,
		Dir:    filepath.Dir(path),
	}
}

func FindConfig() (string, string) {
	home, err := os.UserHomeDir()
	if err != nil {
		panic(err)
	}

	paths := []string{
		filepath.Join(home, ".config", "ovpn-unlock", "config.yml"),
		"/etc/ovpn-unlock/config.yml",
	}

	for _, path := range paths {
		if _, err := os.Stat(path); err == nil {
			return path, filepath.Dir(path)
		}
	}

	panic("config.yml not found")
}
