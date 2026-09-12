module remote-systemd-toggle/remote-systemd-toggle

go 1.27.0

require (
	golang.org/x/term v0.46.0
	remote-systemd-toggle/common v0.0.0
)

require (
	golang.org/x/sys v0.48.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

replace remote-systemd-toggle/common => ../common
