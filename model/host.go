package model

import "net"

// Host represents a network host as synthesized from NetBox data.
// It is the shared data model passed to each generator.
type Host struct {
	Name string // fully qualified domain name, no trailing dot (e.g. "host1.example.com")
	IPv4 net.IP
	IPv6 net.IP
	MAC  string // hardware ethernet address for DHCP (e.g. "00:11:22:33:44:55"), may be empty
}
