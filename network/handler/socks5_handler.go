package handler

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"strings"

	"github.com/DesKaOne/deskaone-sdk/proxy"
)

func Socks5HandlerConn(
	conn net.Conn,
	proxyConfig *proxy.ProxyConfig,
	host string,
	port int,
) error {
	if conn == nil {
		return fmt.Errorf("nil connection")
	}

	if proxyConfig == nil {
		return fmt.Errorf("nil proxy config")
	}

	host = strings.TrimSpace(host)
	if host == "" {
		return fmt.Errorf("empty destination host")
	}

	if len(host) > 255 {
		return fmt.Errorf("host too long")
	}

	if port <= 0 || port > 65535 {
		return fmt.Errorf("invalid destination port: %d", port)
	}

	username := proxyConfig.Username
	password := proxyConfig.Password

	methods := []byte{0x00}
	if username != nil && password != nil {
		if len(*username) > 255 {
			return fmt.Errorf("socks username too long")
		}
		if len(*password) > 255 {
			return fmt.Errorf("socks password too long")
		}

		methods = append(methods, 0x02)
	}

	// Method negotiation:
	// VER, NMETHODS, METHODS...
	if _, err := conn.Write(append([]byte{0x05, byte(len(methods))}, methods...)); err != nil {
		return err
	}

	resp := make([]byte, 2)
	if _, err := io.ReadFull(conn, resp); err != nil {
		return err
	}

	if resp[0] != 0x05 {
		return fmt.Errorf("invalid socks version: %d", resp[0])
	}

	if resp[1] == 0xFF {
		return fmt.Errorf("socks method rejected")
	}

	if resp[1] != 0x00 && resp[1] != 0x02 {
		return fmt.Errorf("unsupported socks auth method selected: %d", resp[1])
	}

	if resp[1] == 0x02 {
		if username == nil || password == nil {
			return fmt.Errorf("socks proxy requested auth but credentials are missing")
		}

		u := []byte(*username)
		p := []byte(*password)

		buf := make([]byte, 0, 3+len(u)+len(p))
		buf = append(buf, 0x01, byte(len(u)))
		buf = append(buf, u...)
		buf = append(buf, byte(len(p)))
		buf = append(buf, p...)

		if _, err := conn.Write(buf); err != nil {
			return err
		}

		auth := make([]byte, 2)
		if _, err := io.ReadFull(conn, auth); err != nil {
			return err
		}

		if auth[0] != 0x01 || auth[1] != 0x00 {
			return fmt.Errorf("socks auth failed")
		}
	}

	// CONNECT request:
	// VER, CMD, RSV, ATYP, DST.ADDR, DST.PORT
	req := make([]byte, 0, 7+len(host))
	req = append(req, 0x05, 0x01, 0x00, 0x03, byte(len(host)))
	req = append(req, host...)

	pb := make([]byte, 2)
	binary.BigEndian.PutUint16(pb, uint16(port))
	req = append(req, pb...)

	if _, err := conn.Write(req); err != nil {
		return err
	}

	head := make([]byte, 4)
	if _, err := io.ReadFull(conn, head); err != nil {
		return err
	}

	if head[0] != 0x05 {
		return fmt.Errorf("invalid socks response version: %d", head[0])
	}

	if head[1] != 0x00 {
		return fmt.Errorf("socks connect failed: %s", socks5ReplyError(head[1]))
	}

	if head[2] != 0x00 {
		return fmt.Errorf("invalid socks reserved byte: %d", head[2])
	}

	var skip int

	switch head[3] {
	case 0x01:
		// IPv4
		skip = 4

	case 0x04:
		// IPv6
		skip = 16

	case 0x03:
		// Domain
		l := make([]byte, 1)
		if _, err := io.ReadFull(conn, l); err != nil {
			return err
		}
		skip = int(l[0])

	default:
		return fmt.Errorf("unsupported socks address type: %d", head[3])
	}

	if _, err := io.ReadFull(conn, make([]byte, skip+2)); err != nil {
		return err
	}

	return nil
}

func socks5ReplyError(code byte) string {
	switch code {
	case 0x01:
		return "general SOCKS server failure"
	case 0x02:
		return "connection not allowed by ruleset"
	case 0x03:
		return "network unreachable"
	case 0x04:
		return "host unreachable"
	case 0x05:
		return "connection refused"
	case 0x06:
		return "TTL expired"
	case 0x07:
		return "command not supported"
	case 0x08:
		return "address type not supported"
	default:
		return fmt.Sprintf("unknown error code %d", code)
	}
}
