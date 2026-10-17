package handler

import (
	"encoding/binary"
	"io"
	"net"
	"testing"

	"github.com/DesKaOne/deskaone-sdk/proxy"
)

func TestSocks5HandlerSendsHostnameForRemoteDNS(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	cfg, err := proxy.NewProxyConfig(proxy.ProxyTypeSOCKS5, "127.0.0.1", 1080, nil, nil)
	if err != nil {
		t.Fatalf("NewProxyConfig() error = %v", err)
	}

	serverErr := make(chan error, 1)
	go func() {
		defer serverConn.Close()

		greeting := make([]byte, 3)
		if _, err := io.ReadFull(serverConn, greeting); err != nil {
			serverErr <- err
			return
		}
		if greeting[0] != 0x05 || greeting[1] != 0x01 || greeting[2] != 0x00 {
			serverErr <- errUnexpectedBytes("greeting", greeting)
			return
		}
		if _, err := serverConn.Write([]byte{0x05, 0x00}); err != nil {
			serverErr <- err
			return
		}

		header := make([]byte, 5)
		if _, err := io.ReadFull(serverConn, header); err != nil {
			serverErr <- err
			return
		}
		if header[0] != 0x05 || header[1] != 0x01 || header[2] != 0x00 || header[3] != 0x03 {
			serverErr <- errUnexpectedBytes("CONNECT header", header)
			return
		}

		hostLen := int(header[4])
		hostAndPort := make([]byte, hostLen+2)
		if _, err := io.ReadFull(serverConn, hostAndPort); err != nil {
			serverErr <- err
			return
		}
		host := string(hostAndPort[:hostLen])
		if host != "example.test" {
			serverErr <- errUnexpectedBytes("destination hostname", []byte(host))
			return
		}
		if got := binary.BigEndian.Uint16(hostAndPort[hostLen:]); got != 443 {
			serverErr <- errUnexpectedBytes("destination port", []byte{byte(got >> 8), byte(got)})
			return
		}

		// SOCKS5 success reply with an IPv4 bound address and port.
		_, err := serverConn.Write([]byte{0x05, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		serverErr <- err
	}()

	if err := Socks5HandlerConn(clientConn, cfg, "example.test", 443); err != nil {
		t.Fatalf("Socks5HandlerConn() error = %v", err)
	}
	if err := <-serverErr; err != nil {
		t.Fatalf("mock SOCKS5 server error = %v", err)
	}
}

type unexpectedBytesError struct {
	label string
	got   []byte
}

func (e unexpectedBytesError) Error() string {
	return e.label + " contained unexpected bytes"
}

func errUnexpectedBytes(label string, got []byte) error {
	return unexpectedBytesError{label: label, got: append([]byte(nil), got...)}
}
