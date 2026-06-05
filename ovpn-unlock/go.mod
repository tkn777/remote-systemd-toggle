module ovpn-unlock/ovpn-unlock

go 1.26.0

require (
	golang.org/x/term v0.43.0
	ovpn-unlock/common v0.0.0
)

require (
	golang.org/x/sys v0.45.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

replace ovpn-unlock/common => ../common
