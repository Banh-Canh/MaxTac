package network

import (
	"fmt"
	"net"
)

func ValidateTargetCIDR(v string) error {
	if _, _, err := net.ParseCIDR(v); err == nil {
		return nil // valid CIDR (v4 or v6)
	}
	if ip := net.ParseIP(v); ip != nil {
		return nil // valid single IP (v4 or v6)
	}
	return fmt.Errorf("invalid targetCidr %q: must be IPv4/IPv6 address or CIDR", v)
}
