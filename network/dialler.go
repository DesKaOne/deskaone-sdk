package network

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/DesKaOne/deskaone-sdk/network/handler"
	p "github.com/DesKaOne/deskaone-sdk/proxy"
)

func dialDirect(
	ctx context.Context,
	addr string,
	timeout time.Duration,
	localAddr *net.TCPAddr,
) (net.Conn, error) {
	dialer := &net.Dialer{
		Timeout:   timeout,
		KeepAlive: 30 * time.Second,
		LocalAddr: localAddr,
	}

	return dialer.DialContext(ctx, "tcp", addr)
}

func dialViaProxy(
	ctx context.Context,
	proxyConfig *p.ProxyConfig,
	dstHost string,
	dstPort int,
	timeout time.Duration,
	localAddr *net.TCPAddr,
) (net.Conn, error) {

	if proxyConfig == nil {
		return nil, fmt.Errorf("nil proxy config")
	}

	if !proxyConfig.IsValid() {
		return nil, fmt.Errorf("invalid proxy config: %s", proxyConfig.String())
	}

	if dstHost == "" {
		return nil, fmt.Errorf("empty destination host")
	}

	if dstPort <= 0 || dstPort > 65535 {
		return nil, fmt.Errorf("invalid destination port: %d", dstPort)
	}

	proxyAddr := net.JoinHostPort(proxyConfig.Host, strconv.Itoa(proxyConfig.Port))

	conn, err := dialDirect(ctx, proxyAddr, timeout, localAddr)
	if err != nil {
		return nil, err
	}

	if timeout > 0 {
		_ = conn.SetDeadline(time.Now().Add(timeout))
	}

	var handlerErr error

	switch proxyConfig.Type {
	case p.ProxyTypeHTTP:
		var wrappedConn net.Conn

		wrappedConn, handlerErr = handler.HttpHandlerConn(
			conn,
			proxyConfig,
			dstHost,
			dstPort,
			timeout,
		)

		if handlerErr == nil {
			if wrappedConn == nil {
				_ = conn.Close()
				return nil, fmt.Errorf("http proxy handler returned nil connection")
			}

			conn = wrappedConn
		}

	case p.ProxyTypeSOCKS5:
		handlerErr = handler.Socks5HandlerConn(
			conn,
			proxyConfig,
			dstHost,
			dstPort,
		)

	case p.ProxyTypeSOCKS4:
		handlerErr = handler.Socks4HandlerConn(
			conn,
			proxyConfig,
			dstHost,
			dstPort,
		)

	default:
		handlerErr = fmt.Errorf("unsupported proxy type: %s", proxyConfig.Type)
	}

	if handlerErr != nil {
		_ = conn.Close()
		return nil, handlerErr
	}

	if conn == nil {
		return nil, fmt.Errorf("proxy tunnel connection is nil")
	}

	_ = conn.SetDeadline(time.Time{})

	return conn, nil
}

func dialTransport(
	ctx context.Context,
	host string,
	port int,
	timeout time.Duration,
	picker p.ProxyPicker,
	localAddr *net.TCPAddr,
) (net.Conn, error) {
	host = strings.TrimSpace(host)
	if host == "" {
		return nil, fmt.Errorf("empty destination host")
	}

	if port <= 0 || port > 65535 {
		return nil, fmt.Errorf("invalid destination port: %d", port)
	}

	dstAddr := net.JoinHostPort(host, strconv.Itoa(port))

	if picker != nil {
		proxyConfig, err := picker()
		if err != nil {
			return nil, err
		}

		if proxyConfig != nil {
			return dialViaProxy(ctx, proxyConfig, host, port, timeout, localAddr)
		}
	}

	return dialDirect(ctx, dstAddr, timeout, localAddr)
}
