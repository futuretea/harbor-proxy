package proxy

import (
	"net"
)

// stripPort removes the port from a host string using net.SplitHostPort
// Handles IPv4, IPv6, and hostname formats correctly
func stripPort(hostport string) string {
	host, _, err := net.SplitHostPort(hostport)
	if err != nil {
		// If SplitHostPort fails, it means there's no port or invalid format
		// Return original string (likely just a hostname)
		return hostport
	}
	return host
}
