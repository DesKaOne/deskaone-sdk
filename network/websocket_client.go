package network

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/deskaone/deskaone-sdk/proxy"
)

const (
	websocketGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

	wsOpContinuation = 0x0
	wsOpText         = 0x1
	wsOpBinary       = 0x2
	wsOpClose        = 0x8
	wsOpPing         = 0x9
	wsOpPong         = 0xA
)

type WebSocketClient struct {
	tcp     *TCPClient
	timeout time.Duration

	readTimeout time.Duration

	mu       sync.Mutex
	isClosed atomic.Bool

	maxPayloadSize  int64
	disableAutoPong bool
}

type WebSocketConfig struct {
	Timeout   time.Duration
	Picker    proxy.ProxyPicker
	LocalBind *LocalBindTCP

	Headers map[string]string

	UserAgent string

	PingInterval   time.Duration
	PingPayload    []byte
	MaxPayloadSize int64

	ReadTimeout time.Duration

	DisableAutoPong bool
}

type WebSocketMessage struct {
	Opcode byte
	Data   []byte
}

type WebSocketCloseError struct {
	Code   uint16
	Reason string
}

func (e *WebSocketCloseError) Error() string {
	if e == nil {
		return "websocket closed"
	}

	if e.Reason != "" {
		return fmt.Sprintf("websocket closed: code=%d reason=%s", e.Code, e.Reason)
	}

	return fmt.Sprintf("websocket closed: code=%d", e.Code)
}

func parseWebSocketClosePayload(payload []byte) (uint16, string) {
	if len(payload) < 2 {
		return 1000, ""
	}

	code := binary.BigEndian.Uint16(payload[:2])
	reason := ""

	if len(payload) > 2 {
		reason = string(payload[2:])
	}

	return code, reason
}

func NewWebSocketClient(
	ctx context.Context,
	rawURL string,
	conf *WebSocketConfig,
) (*WebSocketClient, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	if conf == nil {
		conf = &WebSocketConfig{}
	}

	if conf.MaxPayloadSize <= 0 {
		conf.MaxPayloadSize = 16 * 1024 * 1024
	}

	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return nil, err
	}

	if u.Scheme != "ws" && u.Scheme != "wss" {
		return nil, fmt.Errorf("unsupported websocket scheme: %s", u.Scheme)
	}

	host := u.Hostname()
	if host == "" {
		return nil, fmt.Errorf("empty websocket host")
	}

	port := defaultWebSocketPort(u.Scheme)
	if u.Port() != "" {
		port, err = strconv.Atoi(u.Port())
		if err != nil {
			return nil, fmt.Errorf("invalid websocket port: %s", u.Port())
		}
	}

	isTLS := u.Scheme == "wss"

	tcp, err := NewTCPClientAutoTLS(
		ctx,
		host,
		port,
		conf.Timeout,
		conf.Picker,
		isTLS,
		conf.LocalBind,
	)
	if err != nil {
		return nil, err
	}

	readTimeout := conf.ReadTimeout
	if readTimeout == 0 && conf.PingInterval > 0 {
		readTimeout = conf.PingInterval * 3
	}

	client := &WebSocketClient{
		tcp:             tcp,
		timeout:         conf.Timeout,
		readTimeout:     readTimeout,
		maxPayloadSize:  conf.MaxPayloadSize,
		disableAutoPong: conf.DisableAutoPong,
	}

	if err := client.handshake(u, conf); err != nil {
		_ = tcp.Close()
		return nil, err
	}

	if conf.PingInterval > 0 {
		client.StartPing(ctx, conf.PingInterval, conf.PingPayload)
	}

	return client, nil
}

func (c *WebSocketClient) handshake(u *url.URL, conf *WebSocketConfig) error {
	key, err := generateWebSocketKey()
	if err != nil {
		return err
	}

	path := u.RequestURI()
	if path == "" {
		path = "/"
	}

	hostHeader := u.Host
	if hostHeader == "" {
		hostHeader = u.Hostname()
	}

	userAgent := conf.UserAgent
	if userAgent == "" {
		userAgent = defaultHTTPUserAgent
	}

	var b bytes.Buffer

	b.WriteString("GET ")
	b.WriteString(path)
	b.WriteString(" HTTP/1.1\r\n")

	writeWSHeaderIfMissing(&b, conf.Headers, "Host", hostHeader)
	writeWSHeaderIfMissing(&b, conf.Headers, "Upgrade", "websocket")
	writeWSHeaderIfMissing(&b, conf.Headers, "Connection", "Upgrade")
	writeWSHeaderIfMissing(&b, conf.Headers, "Sec-WebSocket-Key", key)
	writeWSHeaderIfMissing(&b, conf.Headers, "Sec-WebSocket-Version", "13")
	writeWSHeaderIfMissing(&b, conf.Headers, "User-Agent", userAgent)

	for k, v := range conf.Headers {
		keyName := strings.TrimSpace(k)
		value := strings.TrimSpace(v)

		if keyName == "" {
			continue
		}

		b.WriteString(keyName)
		b.WriteString(": ")
		b.WriteString(value)
		b.WriteString("\r\n")
	}

	b.WriteString("\r\n")

	if err := c.tcp.WriteFull(b.Bytes()); err != nil {
		return err
	}

	if c.timeout > 0 {
		_ = c.tcp.conn.SetReadDeadline(time.Now().Add(c.timeout))
		defer c.tcp.conn.SetReadDeadline(time.Time{})
	}

	reader := bufio.NewReader(c.tcp.conn)

	statusLine, err := readHTTPLine(reader)
	if err != nil {
		return err
	}

	statusCode, err := parseHTTPStatusCode(statusLine)
	if err != nil {
		return err
	}

	if statusCode != 101 {
		headers, _ := readHTTPHeaders(reader)
		return fmt.Errorf("websocket upgrade failed: status=%d location=%q", statusCode, getHeader(headers, "Location"))
	}

	headers, err := readHTTPHeaders(reader)
	if err != nil {
		return err
	}

	upgrade := getHeader(headers, "Upgrade")
	if !strings.EqualFold(upgrade, "websocket") {
		return fmt.Errorf("invalid websocket Upgrade header: %q", upgrade)
	}

	connection := strings.ToLower(getHeader(headers, "Connection"))
	if !strings.Contains(connection, "upgrade") {
		return fmt.Errorf("invalid websocket Connection header: %q", connection)
	}

	expectedAccept := computeWebSocketAccept(key)
	actualAccept := strings.TrimSpace(getHeader(headers, "Sec-WebSocket-Accept"))

	if actualAccept != expectedAccept {
		return fmt.Errorf("invalid websocket accept key")
	}

	return nil
}

func (c *WebSocketClient) SendText(text string) error {
	return c.WriteFrame(wsOpText, []byte(text))
}

func (c *WebSocketClient) SendBinary(data []byte) error {
	return c.WriteFrame(wsOpBinary, data)
}

func (c *WebSocketClient) Ping(data []byte) error {
	if len(data) > 125 {
		return fmt.Errorf("websocket ping payload too large")
	}

	return c.WriteFrame(wsOpPing, data)
}

func (c *WebSocketClient) Pong(data []byte) error {
	if len(data) > 125 {
		return fmt.Errorf("websocket pong payload too large")
	}

	return c.WriteFrame(wsOpPong, data)
}

func (c *WebSocketClient) Close() error {
	if c == nil {
		return nil
	}

	if c.isClosed.Swap(true) {
		return nil
	}

	if c.tcp == nil {
		return nil
	}

	_ = c.writeCloseFrame(1000, "normal closure")
	return c.tcp.Close()
}

func (c *WebSocketClient) WriteFrame(opcode byte, payload []byte) error {
	if c == nil || c.tcp == nil || c.isClosed.Load() {
		return ErrTCPClientClosed
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	return c.writeFrameLocked(opcode, payload)
}

func (c *WebSocketClient) writeFrameLocked(opcode byte, payload []byte) error {
	if c.timeout > 0 {
		_ = c.tcp.conn.SetWriteDeadline(time.Now().Add(c.timeout))
		defer c.tcp.conn.SetWriteDeadline(time.Time{})
	}

	frame, err := buildClientWebSocketFrame(opcode, payload)
	if err != nil {
		return err
	}

	return c.tcp.WriteFull(frame)
}

func (c *WebSocketClient) ReadMessage() (*WebSocketMessage, error) {
	if c == nil || c.tcp == nil || c.isClosed.Load() {
		return nil, ErrTCPClientClosed
	}

	var fragmentedOpcode byte
	var fragmentedPayload []byte

	for {
		if c.readTimeout > 0 {
			_ = c.tcp.conn.SetReadDeadline(time.Now().Add(c.readTimeout))
		}

		frame, err := readWebSocketFrameWithLimit(c.tcp.conn, c.maxPayloadSize)

		if c.readTimeout > 0 {
			_ = c.tcp.conn.SetReadDeadline(time.Time{})
		}

		if err != nil {
			c.isClosed.Store(true)
			return nil, err
		}

		switch frame.Opcode {
		case wsOpPing:
			if !c.disableAutoPong {
				_ = c.Pong(frame.Payload)
			}
			continue

		case wsOpPong:
			return &WebSocketMessage{
				Opcode: wsOpPong,
				Data:   frame.Payload,
			}, nil

		case wsOpClose:
			code, reason := parseWebSocketClosePayload(frame.Payload)

			_ = c.writeCloseFrame(code, reason)
			c.isClosed.Store(true)

			return &WebSocketMessage{
					Opcode: wsOpClose,
					Data:   frame.Payload,
				}, &WebSocketCloseError{
					Code:   code,
					Reason: reason,
				}

		case wsOpText, wsOpBinary:
			if frame.Fin {
				return &WebSocketMessage{
					Opcode: frame.Opcode,
					Data:   frame.Payload,
				}, nil
			}

			fragmentedOpcode = frame.Opcode
			fragmentedPayload = append(fragmentedPayload, frame.Payload...)

			if int64(len(fragmentedPayload)) > c.maxPayloadSize {
				return nil, fmt.Errorf("websocket fragmented payload too large")
			}

		case wsOpContinuation:
			if fragmentedOpcode == 0 {
				return nil, fmt.Errorf("unexpected websocket continuation frame")
			}

			fragmentedPayload = append(fragmentedPayload, frame.Payload...)

			if int64(len(fragmentedPayload)) > c.maxPayloadSize {
				return nil, fmt.Errorf("websocket fragmented payload too large")
			}

			if frame.Fin {
				return &WebSocketMessage{
					Opcode: fragmentedOpcode,
					Data:   fragmentedPayload,
				}, nil
			}

		default:
			return nil, fmt.Errorf("unsupported websocket opcode: %d", frame.Opcode)
		}
	}
}

func (c *WebSocketClient) Listen(
	ctx context.Context,
	onMessage func(WebSocketMessage),
	onError func(error),
	onDone func(),
) {
	if ctx == nil {
		ctx = context.Background()
	}

	go func() {
		defer func() {
			_ = c.Close()
			if onDone != nil {
				onDone()
			}
		}()

		done := make(chan struct{})

		go func() {
			select {
			case <-ctx.Done():
				_ = c.Close()
			case <-done:
			}
		}()

		defer close(done)

		for {
			msg, err := c.ReadMessage()
			if err != nil {
				if _, ok := err.(*WebSocketCloseError); ok {
					return
				}

				if err != io.EOF && onError != nil {
					onError(err)
				}

				return
			}

			if msg != nil && onMessage != nil {
				onMessage(*msg)
			}
		}
	}()
}

func (c *WebSocketClient) SendJSON(v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}

	return c.SendText(string(data))
}

func (c *WebSocketClient) ReadJSON(v any) error {
	msg, err := c.ReadMessage()
	if err != nil {
		return err
	}

	if msg == nil {
		return io.EOF
	}

	if msg.Opcode != wsOpText && msg.Opcode != wsOpBinary {
		return fmt.Errorf("websocket message is not JSON-compatible opcode: %d", msg.Opcode)
	}

	return json.Unmarshal(msg.Data, v)
}

func (c *WebSocketClient) StartPing(
	ctx context.Context,
	interval time.Duration,
	payload []byte,
) {
	if ctx == nil {
		ctx = context.Background()
	}

	if interval <= 0 {
		return
	}

	if len(payload) > 125 {
		payload = payload[:125]
	}

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				_ = c.Close()
				return

			case <-ticker.C:
				if c == nil || c.isClosed.Load() {
					return
				}

				if err := c.Ping(payload); err != nil {
					_ = c.Close()
					return
				}
			}
		}
	}()
}

func (c *WebSocketClient) writeCloseFrame(code uint16, reason string) error {
	payload := make([]byte, 2+len(reason))
	binary.BigEndian.PutUint16(payload[:2], code)
	copy(payload[2:], reason)

	c.mu.Lock()
	defer c.mu.Unlock()

	return c.writeFrameLocked(wsOpClose, payload)
}

type websocketFrame struct {
	Fin     bool
	Opcode  byte
	Masked  bool
	Payload []byte
}

func buildClientWebSocketFrame(opcode byte, payload []byte) ([]byte, error) {
	var b bytes.Buffer

	first := byte(0x80) | (opcode & 0x0F)
	b.WriteByte(first)

	payloadLen := len(payload)

	maskBit := byte(0x80)

	switch {
	case payloadLen <= 125:
		b.WriteByte(maskBit | byte(payloadLen))

	case payloadLen <= 65535:
		b.WriteByte(maskBit | 126)

		var lenBuf [2]byte
		binary.BigEndian.PutUint16(lenBuf[0:2], uint16(payloadLen))
		b.Write(lenBuf[0:2])

	default:
		b.WriteByte(maskBit | 127)

		var lenBuf [8]byte
		binary.BigEndian.PutUint64(lenBuf[0:8], uint64(payloadLen))
		b.Write(lenBuf[0:8])
	}

	var maskKey [4]byte
	if _, err := rand.Read(maskKey[0:4]); err != nil {
		return nil, err
	}

	b.Write(maskKey[0:4])

	maskedPayload := make([]byte, payloadLen)
	for i := 0; i < payloadLen; i++ {
		maskedPayload[i] = payload[i] ^ maskKey[i%4]
	}

	b.Write(maskedPayload)

	return b.Bytes(), nil
}

func readWebSocketFrameWithLimit(r io.Reader, maxPayloadSize int64) (*websocketFrame, error) {
	var header [2]byte

	if _, err := io.ReadFull(r, header[:]); err != nil {
		return nil, err
	}

	fin := header[0]&0x80 != 0
	opcode := header[0] & 0x0F
	masked := header[1]&0x80 != 0
	payloadLen := uint64(header[1] & 0x7F)

	switch payloadLen {
	case 126:
		var ext [2]byte
		if _, err := io.ReadFull(r, ext[:]); err != nil {
			return nil, err
		}
		payloadLen = uint64(binary.BigEndian.Uint16(ext[:]))

	case 127:
		var ext [8]byte
		if _, err := io.ReadFull(r, ext[:]); err != nil {
			return nil, err
		}
		payloadLen = binary.BigEndian.Uint64(ext[:])
	}

	if maxPayloadSize > 0 && payloadLen > uint64(maxPayloadSize) {
		return nil, fmt.Errorf("websocket payload too large: %d > %d", payloadLen, maxPayloadSize)
	}

	if payloadLen > int64Max {
		return nil, fmt.Errorf("websocket payload too large")
	}

	var maskKey [4]byte
	if masked {
		if _, err := io.ReadFull(r, maskKey[:]); err != nil {
			return nil, err
		}
	}

	payload := make([]byte, int(payloadLen))
	if payloadLen > 0 {
		if _, err := io.ReadFull(r, payload); err != nil {
			return nil, err
		}
	}

	if masked {
		for i := 0; i < len(payload); i++ {
			payload[i] ^= maskKey[i%4]
		}
	}

	return &websocketFrame{
		Fin:     fin,
		Opcode:  opcode,
		Masked:  masked,
		Payload: payload,
	}, nil
}

const int64Max = uint64(^uint(0) >> 1)

func generateWebSocketKey() (string, error) {
	var b [16]byte

	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}

	return base64.StdEncoding.EncodeToString(b[:]), nil
}

func computeWebSocketAccept(key string) string {
	h := sha1.New()
	h.Write([]byte(key))
	h.Write([]byte(websocketGUID))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

func writeWSHeaderIfMissing(
	b *bytes.Buffer,
	headers map[string]string,
	key string,
	value string,
) {
	if hasHeader(headers, key) {
		return
	}

	b.WriteString(key)
	b.WriteString(": ")
	b.WriteString(value)
	b.WriteString("\r\n")
}

func defaultWebSocketPort(scheme string) int {
	switch strings.ToLower(scheme) {
	case "wss":
		return 443
	default:
		return 80
	}
}
