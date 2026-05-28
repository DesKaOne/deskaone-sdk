package proxy

import (
	"errors"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"
)

var ErrNoProxyAvailable = errors.New("no proxy available")

type ProxyPicker func() (*ProxyConfig, error)

func NewRoundRobinProxyPicker(proxies []*ProxyConfig) ProxyPicker {
	var index atomic.Uint64

	return func() (*ProxyConfig, error) {
		if len(proxies) == 0 {
			return nil, ErrNoProxyAvailable
		}

		i := index.Add(1) - 1
		return proxies[int(i%uint64(len(proxies)))], nil
	}
}

func NewRandomProxyPicker(proxies []*ProxyConfig) ProxyPicker {
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))

	var mu sync.Mutex

	return func() (*ProxyConfig, error) {
		mu.Lock()
		defer mu.Unlock()

		if len(proxies) == 0 {
			return nil, ErrNoProxyAvailable
		}

		return proxies[rng.Intn(len(proxies))], nil
	}
}

func NewSingleProxyPicker(proxy *ProxyConfig) ProxyPicker {
	return func() (*ProxyConfig, error) {
		if proxy == nil {
			return nil, ErrNoProxyAvailable
		}

		return proxy, nil
	}
}
