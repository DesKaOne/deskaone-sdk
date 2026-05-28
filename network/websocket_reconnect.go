package network

import (
	"context"
	"sync"
	"time"
)

type ReconnectWebSocketClient struct {
	URL    string
	Config *WebSocketConfig

	ReconnectDelay time.Duration
	MaxReconnects  int

	OnConnect    func(*WebSocketClient)
	OnMessage    func(WebSocketMessage)
	OnError      func(error)
	OnDisconnect func(error)

	mu     sync.RWMutex
	client *WebSocketClient
}

func NewReconnectWebSocketClient(
	rawURL string,
	config *WebSocketConfig,
) *ReconnectWebSocketClient {
	return &ReconnectWebSocketClient{
		URL:            rawURL,
		Config:         config,
		ReconnectDelay: 2 * time.Second,
		MaxReconnects:  -1,
	}
}

func (r *ReconnectWebSocketClient) Current() *WebSocketClient {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return r.client
}

func (r *ReconnectWebSocketClient) SendText(text string) error {
	client := r.Current()
	if client == nil {
		return ErrTCPClientClosed
	}

	return client.SendText(text)
}

func (r *ReconnectWebSocketClient) SendJSON(v any) error {
	client := r.Current()
	if client == nil {
		return ErrTCPClientClosed
	}

	return client.SendJSON(v)
}

func (r *ReconnectWebSocketClient) Close() error {
	client := r.Current()
	if client == nil {
		return nil
	}

	return client.Close()
}

func (r *ReconnectWebSocketClient) Run(ctx context.Context) {
	if ctx == nil {
		ctx = context.Background()
	}

	reconnects := 0

	for {
		select {
		case <-ctx.Done():
			_ = r.Close()
			return
		default:
		}

		if r.MaxReconnects >= 0 && reconnects > r.MaxReconnects {
			return
		}

		ws, err := NewWebSocketClient(ctx, r.URL, r.Config)
		if err != nil {
			if r.OnError != nil {
				r.OnError(err)
			}

			reconnects++

			if !sleepContext(ctx, r.ReconnectDelay) {
				return
			}

			continue
		}

		r.mu.Lock()
		r.client = ws
		r.mu.Unlock()

		if r.OnConnect != nil {
			r.OnConnect(ws)
		}

		done := make(chan error, 1)

		ws.Listen(
			ctx,
			func(msg WebSocketMessage) {
				if r.OnMessage != nil {
					r.OnMessage(msg)
				}
			},
			func(err error) {
				done <- err
			},
			func() {
				done <- nil
			},
		)

		var disconnectErr error

		select {
		case <-ctx.Done():
			_ = ws.Close()
			return

		case disconnectErr = <-done:
		}

		if r.OnDisconnect != nil {
			r.OnDisconnect(disconnectErr)
		}

		r.mu.Lock()
		if r.client == ws {
			r.client = nil
		}
		r.mu.Unlock()

		_ = ws.Close()

		reconnects++

		if !sleepContext(ctx, r.ReconnectDelay) {
			return
		}
	}
}

func sleepContext(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return true
	}

	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
