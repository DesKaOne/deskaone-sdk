package network

import (
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"sync/atomic"
	"time"

	"github.com/deskaone/deskaone-sdk/proxy"
)

var ErrTCPClientClosed = errors.New("tcp client closed")

type TCPClient struct {
	conn     net.Conn
	timeout  time.Duration
	isClosed atomic.Bool
}

type LocalBindTCP struct {
	IP   net.IP
	Port int
	Zone string
}

func NewTCPClient(
	ctx context.Context,
	host string,
	port int,
	timeout time.Duration,
	picker proxy.ProxyPicker,
	localBind ...*LocalBindTCP,
) (*TCPClient, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	// Saran: jangan auto-bind kecuali user memang memberi localBind.
	// Auto-bind bisa bikin routing/proxy bermasalah di beberapa mesin.
	conn, err := dialTransport(ctx, host, port, timeout, picker, firstLocalBind(localBind...))
	if err != nil {
		return nil, err
	}

	if tcp := unwrapTCPConn(conn); tcp != nil {
		_ = tcp.SetNoDelay(true)
		_ = tcp.SetKeepAlive(true)
		_ = tcp.SetKeepAlivePeriod(15 * time.Second)
	}

	return &TCPClient{
		conn:    conn,
		timeout: timeout,
	}, nil
}

func (c *TCPClient) LocalAddr() net.Addr {
	if c == nil || c.conn == nil {
		return nil
	}

	return c.conn.LocalAddr()
}

func (c *TCPClient) RemoteAddr() net.Addr {
	if c == nil || c.conn == nil {
		return nil
	}

	return c.conn.RemoteAddr()
}

func (c *TCPClient) Conn() net.Conn {
	if c == nil {
		return nil
	}

	return c.conn
}

func (c *TCPClient) IsClosed() bool {
	return c == nil || c.isClosed.Load()
}

func (c *TCPClient) Close() error {
	if c == nil {
		return nil
	}

	if c.isClosed.Swap(true) {
		return nil
	}

	if c.conn != nil {
		return c.conn.Close()
	}

	return nil
}

func (c *TCPClient) Write(b []byte) (int, error) {
	if c == nil || c.conn == nil || c.isClosed.Load() {
		return 0, ErrTCPClientClosed
	}

	n, err := c.conn.Write(b)
	if err != nil {
		c.isClosed.Store(true)
	}

	return n, err
}

func (c *TCPClient) WriteFull(b []byte) error {
	for len(b) > 0 {
		n, err := c.Write(b)
		if err != nil {
			return err
		}

		if n == 0 {
			return io.ErrShortWrite
		}

		b = b[n:]
	}

	return nil
}

func (c *TCPClient) Read(b []byte) (int, error) {
	if c == nil || c.conn == nil || c.isClosed.Load() {
		return 0, ErrTCPClientClosed
	}

	n, err := c.conn.Read(b)
	if err != nil {
		c.isClosed.Store(true)
	}

	return n, err
}

func (c *TCPClient) ReadFull(n int) ([]byte, error) {
	if c == nil || c.conn == nil || c.isClosed.Load() {
		return nil, ErrTCPClientClosed
	}

	if n <= 0 {
		return []byte{}, nil
	}

	buf := make([]byte, n)

	if c.timeout > 0 {
		_ = c.conn.SetReadDeadline(time.Now().Add(c.timeout))
		defer c.conn.SetReadDeadline(time.Time{})
	}

	_, err := io.ReadFull(c.conn, buf)
	if err != nil {
		c.isClosed.Store(true)
		return buf, err
	}

	return buf, nil
}

func (c *TCPClient) Listen(
	ctx context.Context,
	onData func([]byte),
	onError func(error),
	onDone func(),
) {
	if ctx == nil {
		ctx = context.Background()
	}

	client := c

	go func() {
		var done atomic.Bool

		callDone := func() {
			if done.CompareAndSwap(false, true) {
				if onDone != nil {
					onDone()
				}
			}
		}

		go func() {
			<-ctx.Done()
			_ = client.Close()
		}()

		buf := make([]byte, 16*1024)

		for {
			if client == nil || client.isClosed.Load() {
				callDone()
				return
			}

			n, err := client.Read(buf)
			if err != nil {
				if errors.Is(err, ErrTCPClientClosed) || errors.Is(err, io.EOF) {
					callDone()
					return
				}

				if ne, ok := err.(net.Error); ok && ne.Timeout() {
					time.Sleep(20 * time.Millisecond)
					continue
				}

				if onError != nil {
					onError(err)
				}

				callDone()
				return
			}

			if n > 0 && onData != nil {
				data := append([]byte(nil), buf[:n]...)
				onData(data)
			}
		}
	}()
}

func discoverPreferredLocalIP() (net.IP, error) {
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}

	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 {
			continue
		}

		if iface.Flags&net.FlagLoopback != 0 {
			continue
		}

		name := strings.ToLower(iface.Name)
		if strings.Contains(name, "vpn") ||
			strings.Contains(name, "wireguard") ||
			strings.HasPrefix(name, "wg") {
			continue
		}

		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}

		for _, addr := range addrs {
			ipStr := strings.Split(addr.String(), "/")[0]
			ip := net.ParseIP(ipStr)

			if ip != nil && ip.To4() != nil {
				return ip, nil
			}
		}
	}

	return nil, nil
}

func firstLocalBind(bind ...*LocalBindTCP) *net.TCPAddr {
	if len(bind) == 0 || bind[0] == nil || bind[0].IP == nil {
		return nil
	}

	return &net.TCPAddr{
		IP:   bind[0].IP,
		Port: bind[0].Port,
		Zone: bind[0].Zone,
	}
}

func unwrapTCPConn(conn net.Conn) *net.TCPConn {
	if tcp, ok := conn.(*net.TCPConn); ok {
		return tcp
	}

	return nil
}
