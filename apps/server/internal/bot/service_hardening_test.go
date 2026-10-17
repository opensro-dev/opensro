package bot_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"opensro.online/server/internal/bot"
	"opensro.online/server/internal/transport"
)

func TestBrainPacesRebirthAndResetsAfterLife(t *testing.T) {
	brain := newBrain()
	actions := &fakeActions{}
	world := fakeWorld{self: bot.Self{Dead: true}}
	for i := 0; i < 30; i++ {
		if err := brain.Tick(world, actions); err != nil {
			t.Fatal(err)
		}
	}
	if actions.respawns != 3 {
		t.Fatalf("requests = %d, want 3", actions.respawns)
	}
	world.self = bot.Self{HP: 100, MaxHP: 100}
	if err := brain.Tick(world, actions); err != nil {
		t.Fatal(err)
	}
	world.self.Dead = true
	if err := brain.Tick(world, actions); err != nil {
		t.Fatal(err)
	}
	if actions.respawns != 4 {
		t.Fatalf("new death did not reset pacing: %d", actions.respawns)
	}
}

func TestBrainTargetOrderAndNearestThree(t *testing.T) {
	monsters := []bot.Actor{
		{GID: 9, Region: 1, X: 900}, {GID: 4, Region: 1, X: 40},
		{GID: 3, Region: 1, X: 30}, {GID: 2, Region: 1, X: 20},
		{GID: 1, Region: 1, X: -20},
	}
	for _, stochastic := range []bool{false, true} {
		for seed := int64(1); seed <= 100; seed++ {
			var want uint32
			for rotation := range monsters {
				input := append(append([]bot.Actor{}, monsters[rotation:]...), monsters[:rotation]...)
				brain := bot.NewBrain(bot.BrainConfig{Seed: seed, Stochastic: stochastic})
				actions := &fakeActions{}
				world := fakeWorld{self: bot.Self{Region: 1, HP: 100, MaxHP: 100}, monsters: input}
				if err := brain.Tick(world, actions); err != nil {
					t.Fatal(err)
				}
				got := brain.Target().GID
				if got > 3 {
					t.Fatalf("picked %d outside nearest three", got)
				}
				if !stochastic && got != 1 {
					t.Fatalf("tie picked %d, want 1", got)
				}
				if rotation == 0 {
					want = got
				} else if got != want {
					t.Fatalf("seed %d rotation %d: got %d want %d", seed, rotation, got, want)
				}
			}
		}
	}
}

func TestBrainLootTieUsesGID(t *testing.T) {
	for _, ids := range [][]uint32{{2, 1}, {1, 2}} {
		brain := newBrain()
		actions := &fakeActions{}
		world := fakeWorld{self: bot.Self{GID: 7, Region: 1, HP: 100, MaxHP: 100}}
		for _, gid := range ids {
			world.items = append(world.items, bot.GroundItem{GID: gid, HasOwner: true, OwnerJID: 7, Region: 1, IsGold: true, X: 10})
		}
		if err := brain.Tick(world, actions); err != nil {
			t.Fatal(err)
		}
		if len(actions.pickups) != 1 || actions.pickups[0] != 1 {
			t.Fatalf("pickups = %v", actions.pickups)
		}
	}
}

func TestSocketSerializesActionsAndKeepalive(t *testing.T) {
	const writers, each, pings = 8, 50, 20
	result := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			result <- err
			return
		}
		defer conn.Close()
		_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
		for i := 0; i < pings; i++ {
			if err := conn.WriteMessage(websocket.BinaryMessage, (transport.Frame{Opcode: transport.OpPing}).Encode()); err != nil {
				result <- err
				return
			}
		}
		pongCount, actionCount := 0, 0
		for i := 0; i < writers*each+pings; i++ {
			_, data, err := conn.ReadMessage()
			if err != nil {
				result <- err
				return
			}
			frame, err := transport.DecodeFrame(data)
			if err != nil {
				result <- err
				return
			}
			if frame.Opcode == transport.OpPong {
				pongCount++
			} else {
				actionCount++
			}
		}
		if pongCount != pings || actionCount != writers*each {
			result <- fmt.Errorf("pongs=%d actions=%d", pongCount, actionCount)
			return
		}
		result <- nil
	}))
	defer server.Close()
	socket, err := bot.DialWebSocket(context.Background(), "ws"+strings.TrimPrefix(server.URL, "http"), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer socket.Close()
	var group sync.WaitGroup
	for i := 0; i < writers; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			for j := 0; j < each; j++ {
				if err := socket.Write(0x72CD, []byte{1}); err != nil {
					t.Error(err)
					return
				}
			}
		}()
	}
	group.Wait()
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(12 * time.Second):
		t.Fatal("server did not receive all frames")
	}
}
