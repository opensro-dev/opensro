package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"opensro.online/server/internal/bot"
)

func TestRunBrainStopsOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	world := newBotWorld(100)
	err := runBrain(ctx, nil, world, &botActions{world: world}, bot.NewBrain(bot.BrainConfig{}), nil, time.Millisecond)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v", err)
	}
}

func TestRunBrainReportsLostConnection(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err == nil {
			conn.Close()
		}
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	socket, err := bot.DialWebSocket(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer socket.Close()
	world := newBotWorld(100)
	err = runBrain(ctx, socket, world, &botActions{world: world}, bot.NewBrain(bot.BrainConfig{}), nil, time.Millisecond)
	if err == nil || ctx.Err() != nil {
		t.Fatalf("lost connection error = %v, context = %v", err, ctx.Err())
	}
}
