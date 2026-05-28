package proxy

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
)

func ParseProxyConfig(proxyStr string) (*ProxyConfig, error) {
	proxyType, host, port, username, password, err := parseProxyString(proxyStr)
	if err != nil {
		return nil, err
	}

	return NewProxyConfig(proxyType, host, port, username, password)
}

func parseProxyString(proxyStr string) (ProxyType, string, int, *string, *string, error) {
	raw := strings.TrimSpace(proxyStr)
	if raw == "" {
		return "", "", 0, nil, nil, fmt.Errorf("empty proxy string")
	}

	u, err := url.Parse(raw)
	if err != nil {
		return "", "", 0, nil, nil, fmt.Errorf("invalid proxy string")
	}

	proxyType, err := ProxyTypeFromString(u.Scheme)
	if err != nil {
		return "", "", 0, nil, nil, err
	}

	host := strings.TrimSpace(u.Hostname())
	if host == "" {
		return "", "", 0, nil, nil, fmt.Errorf("invalid proxy host")
	}

	portStr := strings.TrimSpace(u.Port())
	if portStr == "" {
		return "", "", 0, nil, nil, fmt.Errorf("missing proxy port")
	}

	port, err := strconv.Atoi(portStr)
	if err != nil {
		return "", "", 0, nil, nil, fmt.Errorf("invalid proxy port")
	}

	var username *string
	var password *string

	if u.User != nil {
		user := u.User.Username()
		pass, hasPassword := u.User.Password()

		if user == "" || !hasPassword || pass == "" {
			return "", "", 0, nil, nil, fmt.Errorf("invalid proxy auth")
		}

		username = &user
		password = &pass
	}

	// Optional: validasi host:port secara ekstra.
	if _, _, err := net.SplitHostPort(net.JoinHostPort(host, strconv.Itoa(port))); err != nil {
		return "", "", 0, nil, nil, fmt.Errorf("invalid proxy address")
	}

	return proxyType, host, port, username, password, nil
}

func (p ProxyConfig) UUID() string {
	user := ""
	pass := ""
	if p.Username != nil {
		user = *p.Username
	}
	if p.Password != nil {
		pass = *p.Password
	}
	raw := fmt.Sprintf("%s|%d|%s|%s|%s",
		p.Host, p.Port, fmt.Sprint(p.Type), user, pass)
	h := md5.Sum([]byte(raw))
	return hex.EncodeToString(h[:])
}
