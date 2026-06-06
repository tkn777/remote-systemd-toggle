module systemd-service-toggle/systemd-service-toggle

go 1.26.0

require (
	golang.org/x/term v0.43.0
	systemd-service-toggle/common v0.0.0
)

require (
	golang.org/x/sys v0.45.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

replace systemd-service-toggle/common => ../common
