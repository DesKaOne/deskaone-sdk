package handler

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/DesKaOne/deskaone-sdk/proxy"
)

const (
	httpUserAgent     = "DesKaOne/0.0.1"
	maxHTTPHeaderSize = 32 * 1024
)

type preReadConn struct {
	net.Conn
	pre *bytes.Reader
}

func (c *preReadConn) Read(b []byte) (int, error) {
	if c.pre != nil && c.pre.Len() > 0 {
		n, err := c.pre.Read(b)
		if c.pre.Len() == 0 {
			c.pre = nil
		}
		return n, err
	}

	return c.Conn.Read(b)
}

func HttpHandlerConn(
	conn net.Conn,
	proxyConfig *proxy.ProxyConfig,
	dstHost string,
	dstPort int,
	timeout time.Duration,
) (net.Conn, error) {
	if conn == nil {
		return nil, fmt.Errorf("nil connection")
	}

	if proxyConfig == nil {
		return nil, fmt.Errorf("nil proxy config")
	}

	if strings.TrimSpace(dstHost) == "" {
		return nil, fmt.Errorf("empty destination host")
	}

	if dstPort <= 0 || dstPort > 65535 {
		return nil, fmt.Errorf("invalid destination port: %d", dstPort)
	}

	targetAddr := net.JoinHostPort(dstHost, strconv.Itoa(dstPort))

	var b strings.Builder
	b.Grow(256)

	b.WriteString("CONNECT ")
	b.WriteString(targetAddr)
	b.WriteString(" HTTP/1.1\r\n")

	b.WriteString("Host: ")
	b.WriteString(targetAddr)
	b.WriteString("\r\n")

	b.WriteString("User-Agent: ")
	b.WriteString(httpUserAgent)
	b.WriteString("\r\n")

	b.WriteString("Proxy-Connection: Keep-Alive\r\n")

	username := proxyConfig.Username
	password := proxyConfig.Password

	if username != nil && password != nil {
		auth := base64.StdEncoding.EncodeToString([]byte(*username + ":" + *password))
		b.WriteString("Proxy-Authorization: Basic ")
		b.WriteString(auth)
		b.WriteString("\r\n")
	}

	b.WriteString("\r\n")

	if timeout > 0 {
		_ = conn.SetWriteDeadline(time.Now().Add(timeout))
	}

	if _, err := conn.Write([]byte(b.String())); err != nil {
		_ = conn.SetWriteDeadline(time.Time{})
		return nil, err
	}

	_ = conn.SetWriteDeadline(time.Time{})

	headers, leftover, err := readHeaders(conn, timeout)
	if err != nil {
		_ = conn.SetReadDeadline(time.Time{})
		return nil, err
	}

	// Penting: bersihkan deadline setelah CONNECT sukses/gagal dibaca.
	_ = conn.SetReadDeadline(time.Time{})

	code := parseStatusCode(headers)
	if code != 200 {
		authHdr := findHeader(headers, "Proxy-Authenticate")
		msg := sanitizeHeaderForError(headers)

		if authHdr != "" {
			return nil, fmt.Errorf("proxy refused CONNECT: status=%d authenticate=%q response=%q", code, authHdr, msg)
		}

		return nil, fmt.Errorf("proxy refused CONNECT: status=%d response=%q", code, msg)
	}

	if len(leftover) > 0 {
		return &preReadConn{
			Conn: conn,
			pre:  bytes.NewReader(leftover),
		}, nil
	}

	return conn, nil
}

func readHeaders(conn net.Conn, timeout time.Duration) ([]byte, []byte, error) {
	delimiter := []byte("\r\n\r\n")
	buf := make([]byte, 0, 4096)
	tmp := make([]byte, 1024)

	deadline := time.Time{}
	if timeout > 0 {
		deadline = time.Now().Add(timeout)
	}

	for {
		if !deadline.IsZero() {
			_ = conn.SetReadDeadline(deadline)
		}

		n, err := conn.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)

			if len(buf) > maxHTTPHeaderSize {
				return nil, nil, fmt.Errorf("HTTP CONNECT response header too large")
			}

			if idx := bytes.Index(buf, delimiter); idx >= 0 {
				header := buf[:idx+len(delimiter)]
				leftover := buf[idx+len(delimiter):]
				return header, leftover, nil
			}
		}

		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				return nil, nil, fmt.Errorf("HTTP CONNECT header read timed out")
			}

			return nil, nil, err
		}
	}
}

func parseStatusCode(headers []byte) int {
	lineEnd := bytes.Index(headers, []byte("\r\n"))
	if lineEnd < 0 {
		lineEnd = len(headers)
	}

	statusLine := string(headers[:lineEnd])
	parts := strings.Fields(statusLine)
	if len(parts) < 2 {
		return -1
	}

	code, err := strconv.Atoi(parts[1])
	if err != nil {
		return -1
	}

	return code
}

func findHeader(headers []byte, name string) string {
	lines := strings.Split(string(headers), "\r\n")

	for _, line := range lines {
		if line == "" {
			continue
		}

		if i := strings.Index(line, ":"); i > 0 {
			key := strings.TrimSpace(line[:i])
			value := strings.TrimSpace(line[i+1:])

			if strings.EqualFold(key, name) {
				return value
			}
		}
	}

	return ""
}

func sanitizeHeaderForError(headers []byte) string {
	msg := string(headers)

	// Biar error log tidak kebanyakan.
	if len(msg) > 1024 {
		msg = msg[:1024] + "...[truncated]"
	}

	// Jangan bocorkan auth kalau suatu saat ada header sensitif.
	lines := strings.Split(msg, "\r\n")
	for i, line := range lines {
		if strings.HasPrefix(strings.ToLower(line), "proxy-authorization:") {
			lines[i] = "Proxy-Authorization: [redacted]"
		}
		if strings.HasPrefix(strings.ToLower(line), "authorization:") {
			lines[i] = "Authorization: [redacted]"
		}
	}

	return strings.TrimSpace(strings.Join(lines, "\r\n"))
}
