package ip

import (
	"net"
	"net/http"
	"strings"
)

// Gets real IP of the user. Order:
//   - 'X-Forwarded-For' - de facto standard, which is supported by majority of reverse proxies
//   - a custom header defined in the params
//   - req.RemoteAddr
//
// Returns single IP address
func GetRemoteHeader(req *http.Request, customHeaderName string) string {
	header := req.RemoteAddr

	// TODO: to discuss the order of precedence
	possibleIPHeaders := []string{
		req.Header.Get("X-Forwarded-For"),
		req.Header.Get(customHeaderName),
		req.RemoteAddr,
	}

	// pick first with meaningful data
	for _, v := range possibleIPHeaders {
		if v != "" {
			header = v
			break
		}
	}

	// sometimes you get multiple addresses
	addresses := strings.Split(header, ",")
	if ip := net.ParseIP(addresses[0]); ip != nil {
		header = addresses[0]
	}

	return header
}
