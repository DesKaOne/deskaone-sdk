package main

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/DesKaOne/deskaone-sdk/network"
)

func autoPing() {
	ws, err := network.NewWebSocketClient(
		context.Background(),
		"wss://echo.websocket.org",
		&network.WebSocketConfig{
			Timeout:        15 * time.Second,
			ReadTimeout:    60 * time.Second,
			PingInterval:   20 * time.Second,
			PingPayload:    []byte("ping"),
			MaxPayloadSize: 8 * 1024 * 1024,
		},
	)
	if err != nil {
		panic(err)
	}
	defer ws.Close()

	msg, err := ws.ReadMessage()
	if err != nil && err != io.EOF {
		panic(err)
	}

	fmt.Println("opcode:", msg.Opcode)
	fmt.Println("data:", string(msg.Data))

	type Payload struct {
		Type string `json:"type"`
		Data string `json:"data"`
	}

	err = ws.SendJSON(Payload{
		Type: "hello",
		Data: "halo dari DesKaOne SDK",
	})
	if err != nil {
		panic(err)
	}

	msg, err = ws.ReadMessage()
	if err != nil && err != io.EOF {
		panic(err)
	}

	fmt.Println("opcode:", msg.Opcode)
	fmt.Println("data:", string(msg.Data))
}

func autoReconnect() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	rws := network.NewReconnectWebSocketClient(
		"wss://echo.websocket.org",
		&network.WebSocketConfig{
			Timeout:        15 * time.Second,
			ReadTimeout:    60 * time.Second,
			PingInterval:   20 * time.Second,
			MaxPayloadSize: 8 * 1024 * 1024,
		},
	)

	rws.ReconnectDelay = 3 * time.Second
	rws.MaxReconnects = -1

	rws.OnConnect = func(ws *network.WebSocketClient) {
		fmt.Println("connected")

		_ = ws.SendText("hello after connect")
	}

	rws.OnMessage = func(msg network.WebSocketMessage) {
		fmt.Println("message:", string(msg.Data))
	}

	rws.OnError = func(err error) {
		fmt.Println("error:", err)
	}

	rws.OnDisconnect = func(err error) {
		fmt.Println("disconnected:", err)
	}

	rws.Run(ctx)
}

func main() {
	fmt.Println("=== Auto Ping ===")
	autoPing()
	fmt.Println("\n=== Auto Reconnect ===")
	autoReconnect()
}
