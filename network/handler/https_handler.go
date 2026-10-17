package handler

import (
	"crypto/tls"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/DesKaOne/deskaone-sdk/proxy"
)

// HttpsHandlerConn establishes TLS to the HTTPS proxy and then uses HTTP CONNECT.
// The caller remains responsible for closing the original connection on failure.
func HttpsHandlerConn(
	conn net.Conn,
	proxyConfig *proxy.ProxyConfig,
	dstHost string,
	dstPort int,
	timeout time.Duration,
) (net.Conn, error) {
	return httpsHandlerConnWithTLSConfig(conn, proxyConfig, dstHost, dstPort, timeout, nil)
}

// httpsHandlerConnWithTLSConfig permits test CAs without disabling verification.
func httpsHandlerConnWithTLSConfig(
	conn net.Conn,
	proxyConfig *proxy.ProxyConfig,
	dstHost string,
	dstPort int,
	timeout time.Duration,
	tlsConfig *tls.Config,
) (net.Conn, error) {
	if conn == nil {
		return nil, fmt.Errorf("nil connection")
	}
	if proxyConfig == nil {
		return nil, fmt.Errorf("nil proxy config")
	}

	proxyHost := strings.TrimSpace(proxyConfig.Host)
	if proxyHost == "" {
		return nil, fmt.Errorf("empty proxy host")
	}
	if strings.TrimSpace(dstHost) == "" {
		return nil, fmt.Errorf("empty destination host")
	}
	if dstPort <= 0 || dstPort > 65535 {
		return nil, fmt.Errorf("invalid destination port: %d", dstPort)
	}

	if tlsConfig == nil {
		tlsConfig = &tls.Config{}
	} else {
		tlsConfig = tlsConfig.Clone()
	}
	if tlsConfig.ServerName == "" {
		tlsConfig.ServerName = proxyHost
	}
	if tlsConfig.MinVersion == 0 {
		tlsConfig.MinVersion = tls.VersionTLS12
	}

	tlsConn := tls.Client(conn, tlsConfig)
	if timeout > 0 {
		_ = tlsConn.SetDeadline(time.Now().Add(timeout))
	}
	if err := tlsConn.Handshake(); err != nil {
		_ = tlsConn.SetDeadline(time.Time{})
		return nil, fmt.Errorf("HTTPS proxy TLS handshake: %w", err)
	}
	_ = tlsConn.SetDeadline(time.Time{})

	tunnelConn, err := HttpHandlerConn(tlsConn, proxyConfig, dstHost, dstPort, timeout)
	if err != nil {
		return nil, fmt.Errorf("HTTPS proxy CONNECT: %w", err)
	}
	return tunnelConn, nil
}
