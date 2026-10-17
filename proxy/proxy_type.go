package proxy

import (
	"fmt"
	"strings"
)

type ProxyType string

const (
	ProxyTypeHTTP   ProxyType = "http"
	ProxyTypeHTTPS  ProxyType = "https"
	ProxyTypeSOCKS4 ProxyType = "socks4"
	ProxyTypeSOCKS5 ProxyType = "socks5"
)

func (p ProxyType) String() string {
	return string(p)
}

func (p ProxyType) IsValid() bool {
	switch p {
	case ProxyTypeHTTP, ProxyTypeHTTPS, ProxyTypeSOCKS4, ProxyTypeSOCKS5:
		return true
	default:
		return false
	}
}

func ProxyTypeFromString(s string) (ProxyType, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "http":
		return ProxyTypeHTTP, nil
	case "https":
		return ProxyTypeHTTPS, nil
	case "s4", "sock4", "socks4":
		return ProxyTypeSOCKS4, nil
	case "s5", "sock5", "socks5", "socks5h":
		// SOCKS5 always sends the destination hostname to the proxy using
		// ATYP=DOMAINNAME in Socks5HandlerConn, so socks5h is an alias.
		return ProxyTypeSOCKS5, nil
	default:
		return "", fmt.Errorf("invalid proxy type: %s", s)
	}
}
