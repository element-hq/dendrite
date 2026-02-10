package internal

import (
	"context"
	"fmt"
	"net"
	"syscall"
	"time"
)

var (
	ErrDeniedAddress = fmt.Errorf("address is denied")
)

func GetDialer(allowNetworks []string, denyNetworks []string, dialTimeout time.Duration) *net.Dialer {
	if len(allowNetworks) == 0 && len(denyNetworks) == 0 {
		return &net.Dialer{
			Timeout: dialTimeout,
		}
	}

	return &net.Dialer{
		Timeout:        time.Second * 5,
		ControlContext: allowDenyNetworksControl(allowNetworks, denyNetworks),
	}
}

// allowDenyNetworksControl is used to allow/deny access to certain networks
func allowDenyNetworksControl(allowNetworks, denyNetworks []string) func(_ context.Context, network string, address string, conn syscall.RawConn) error {
	return func(_ context.Context, network string, address string, conn syscall.RawConn) error {
		if network != "tcp4" && network != "tcp6" {
			return fmt.Errorf("%s is not a safe network type", network)
		}

		host, _, err := net.SplitHostPort(address)
		if err != nil {
			return fmt.Errorf("%s is not a valid host/port pair: %s", address, err)
		}

		ipaddress := net.ParseIP(host)
		if ipaddress == nil {
			return fmt.Errorf("%s is not a valid IP address", host)
		}

		if !isAllowed(ipaddress, allowNetworks, denyNetworks) {
			return ErrDeniedAddress
		}

		return nil // allow connection
	}
}

func isAllowed(ip net.IP, allowCIDRs []string, denyCIDRs []string) bool {
	if inRange(ip, denyCIDRs) {
		return false
	}
	if inRange(ip, allowCIDRs) {
		return true
	}
	return false // "should never happen"
}

func inRange(ip net.IP, CIDRs []string) bool {
	for i := 0; i < len(CIDRs); i++ {
		cidr := CIDRs[i]
		_, network, err := net.ParseCIDR(cidr)
		if err != nil {
			return false
		}
		if network.Contains(ip) {
			return true
		}
	}

	return false
}
