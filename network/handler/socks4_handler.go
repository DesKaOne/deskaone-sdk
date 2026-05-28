package handler

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"

	"github.com/DesKaOne/deskaone-sdk/proxy"
)

func Socks4HandlerConn(
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

	if port <= 0 || port > 65535 {
		return fmt.Errorf("invalid destination port: %d", port)
	}

	username := ""
	if proxyConfig.Username != nil {
		username = *proxyConfig.Username
	}

	if strings.Contains(username, "\x00") {
		return fmt.Errorf("socks4 userid contains null byte")
	}

	if strings.Contains(host, "\x00") {
		return fmt.Errorf("socks4 host contains null byte")
	}

	ip := net.ParseIP(host)

	req := make([]byte, 0, 9+len(username)+len(host))

	// VN = 0x04, CD = 0x01 CONNECT
	req = append(req, 0x04, 0x01)

	// DSTPORT, big endian
	portBytes := make([]byte, 2)
	binary.BigEndian.PutUint16(portBytes, uint16(port))
	req = append(req, portBytes...)

	if ip4 := ip.To4(); ip4 != nil {
		// SOCKS4 IPv4 mode
		req = append(req, ip4...)
		req = append(req, []byte(username)...)
		req = append(req, 0x00)
	} else {
		// SOCKS4a domain mode
		// DSTIP = 0.0.0.1
		req = append(req, 0x00, 0x00, 0x00, 0x01)
		req = append(req, []byte(username)...)
		req = append(req, 0x00)
		req = append(req, []byte(host)...)
		req = append(req, 0x00)
	}

	if _, err := conn.Write(req); err != nil {
		return err
	}

	resp := make([]byte, 8)
	if _, err := io.ReadFull(conn, resp); err != nil {
		return err
	}

	// SOCKS4 response:
	// VN should be 0x00
	// CD 0x5A means request granted
	if resp[0] != 0x00 {
		return fmt.Errorf("invalid socks4 response version: %d", resp[0])
	}

	if resp[1] != 0x5A {
		return fmt.Errorf("socks4 connect failed: %s", socks4ReplyError(resp[1]))
	}

	return nil
}

func socks4ReplyError(code byte) string {
	switch code {
	case 0x5B:
		return "request rejected or failed"
	case 0x5C:
		return "request failed because client is not running identd"
	case 0x5D:
		return "request failed because client identd could not confirm userid"
	default:
		return "unknown error code " + strconv.Itoa(int(code))
	}
}
