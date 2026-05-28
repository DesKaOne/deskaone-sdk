package main

import (
	"context"
	"fmt"
	"time"

	"github.com/DesKaOne/deskaone-sdk/network"
	"github.com/DesKaOne/deskaone-sdk/proxy"
)

func withProxy() {
	proxyConfig, err := proxy.ParseProxyConfig("socks5://206.123.156.211:4996")
	if err != nil {
		panic(err)
	}

	picker := proxy.NewSingleProxyPicker(proxyConfig)

	client := network.NewHTTPClient(
		15*time.Second,
		picker,
	)

	res, err := client.Get(context.Background(), "https://api.ipify.org?format=json", map[string]string{
		"Accept": "application/json",
	})
	if err != nil {
		panic(err)
	}

	fmt.Println(res.StatusCode)
	fmt.Println(string(res.Body))
}

func direct() {
	client := network.NewHTTPClient(
		15*time.Second,
		nil,
	)

	res, err := client.Get(context.Background(), "https://api.ipify.org?format=json", nil)
	if err != nil {
		panic(err)
	}

	fmt.Println(res.StatusCode)
	fmt.Println(string(res.Body))
}

func main() {
	fmt.Println("=== Direct Connection ===")
	direct()
	fmt.Println("\n=== Via Proxy ===")
	withProxy()
}
