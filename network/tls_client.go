package network

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"strings"
	"time"

	"github.com/deskaone/deskaone-sdk/proxy"
)

type TLSConfig struct {
	ServerName         string
	InsecureSkipVerify bool
	RootCAs            *x509.CertPool
}

func NewTLSClient(
	ctx context.Context,
	host string,
	port int,
	timeout time.Duration,
	picker proxy.ProxyPicker,
	conf *TLSConfig,
	localBind ...*LocalBindTCP,
) (*TCPClient, error) {
	host = strings.TrimSpace(host)
	if host == "" {
		return nil, fmt.Errorf("empty tls host")
	}

	if conf == nil {
		conf = &TLSConfig{}
	}

	client, err := NewTCPClient(ctx, host, port, timeout, picker, localBind...)
	if err != nil {
		return nil, err
	}

	serverName := strings.TrimSpace(conf.ServerName)
	if serverName == "" {
		serverName = host
	}

	cfg := &tls.Config{
		ServerName:         serverName,
		InsecureSkipVerify: conf.InsecureSkipVerify,
		RootCAs:            conf.RootCAs,
	}

	tlsConn := tls.Client(client.conn, cfg)

	if timeout > 0 {
		_ = tlsConn.SetDeadline(time.Now().Add(timeout))
	}

	if err := tlsConn.Handshake(); err != nil {
		_ = tlsConn.Close()
		_ = client.Close()
		return nil, err
	}

	_ = tlsConn.SetDeadline(time.Time{})

	client.conn = tlsConn
	return client, nil
}

func NewTCPClientAutoTLS(
	ctx context.Context,
	host string,
	port int,
	timeout time.Duration,
	picker proxy.ProxyPicker,
	isTLS bool,
	localBind ...*LocalBindTCP,
) (*TCPClient, error) {
	if isTLS {
		return NewTLSClient(ctx, host, port, timeout, picker, &TLSConfig{
			ServerName: host,
		}, localBind...)
	}

	return NewTCPClient(ctx, host, port, timeout, picker, localBind...)
}
