module systemd-service-toggle/systemd-service-toggled

go 1.26.0

require (
	golang.org/x/crypto v0.52.0
	golang.org/x/term v0.43.0
	gopkg.in/yaml.v3 v3.0.1
	systemd-service-toggle/common v0.0.0
)

require golang.org/x/sys v0.45.0 // indirect

replace systemd-service-toggle/common => ../common
