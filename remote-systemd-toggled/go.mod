module remote-systemd-toggle/remote-systemd-toggled

go 1.26.5

require (
	golang.org/x/crypto v0.53.0
	golang.org/x/term v0.44.0
	gopkg.in/yaml.v3 v3.0.1
	remote-systemd-toggle/common v0.0.0
)

require golang.org/x/sys v0.46.0 // indirect

replace remote-systemd-toggle/common => ../common
