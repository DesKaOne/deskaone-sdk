package proxy

import (
	"fmt"
	"net"
	"strconv"
	"strings"
)

type ProxyConfig struct {
	Type     ProxyType
	Host     string
	Port     int
	Username *string
	Password *string
}

func NewProxyConfig(
	proxyType ProxyType,
	host string,
	port int,
	username,
	password *string,
) (*ProxyConfig, error) {
	config := &ProxyConfig{
		Type:     proxyType,
		Host:     strings.TrimSpace(host),
		Port:     port,
		Username: trimStringPtr(username),
		Password: trimStringPtr(password),
	}

	if !config.IsValid() {
		return nil, fmt.Errorf("invalid proxy configuration: %s", config.String())
	}

	return config, nil
}

func (c ProxyConfig) Address() string {
	return net.JoinHostPort(c.Host, strconv.Itoa(c.Port))
}

func (c ProxyConfig) String() string {
	auth := ""
	if c.Username != nil && c.Password != nil {
		auth = *c.Username + ":***@"
	}

	return c.Type.String() + "://" + auth + c.Address()
}

func (c ProxyConfig) URLString() string {
	auth := ""
	if c.Username != nil && c.Password != nil {
		auth = *c.Username + ":" + *c.Password + "@"
	}

	return c.Type.String() + "://" + auth + c.Address()
}

func (c ProxyConfig) IsValid() bool {
	if !c.Type.IsValid() {
		return false
	}

	if strings.TrimSpace(c.Host) == "" {
		return false
	}

	if c.Port <= 0 || c.Port > 65535 {
		return false
	}

	if (c.Username != nil && c.Password == nil) || (c.Username == nil && c.Password != nil) {
		return false
	}

	if c.Username != nil && c.Password != nil {
		if strings.TrimSpace(*c.Username) == "" || strings.TrimSpace(*c.Password) == "" {
			return false
		}
	}

	return true
}

func trimStringPtr(value *string) *string {
	if value == nil {
		return nil
	}

	trimmed := strings.TrimSpace(*value)
	return &trimmed
}
