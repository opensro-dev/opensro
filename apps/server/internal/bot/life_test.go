package bot_test

import (
	"testing"

	"opensro.online/server/internal/bot"
	"opensro.online/server/internal/game/item/wire"
)

func TestParseLife(t *testing.T) {
	dead := wire.ObjectStateRefresh{Gid: 100007, StateType: wire.StateChannelLife, Value: wire.LifeStateDead}.Encode()
	life, ok := bot.ParseLife(dead)
	if !ok || life.GID != 100007 || !life.Dead {
		t.Fatalf("life = %+v ok=%v", life, ok)
	}
	alive := wire.ObjectStateRefresh{Gid: 100007, StateType: wire.StateChannelLife, Value: wire.LifeStateAlive}.Encode()
	if life, ok := bot.ParseLife(alive); !ok || life.Dead {
		t.Fatalf("alive life = %+v ok=%v", life, ok)
	}
	body := wire.ObjectStateRefresh{Gid: 100007, StateType: wire.StateChannelBody, Value: 3}.Encode()
	if _, ok := bot.ParseLife(body); ok {
		t.Fatal("a non-life channel was accepted")
	}
	if _, ok := bot.ParseLife(dead[:3]); ok {
		t.Fatal("a short life body was accepted")
	}
}

func TestEncodeLocalRebirth(t *testing.T) {
	body := bot.EncodeLocalRebirth()
	if len(body) != 1 || body[0] != wire.RebirthAtPresentPoint {
		t.Fatalf("rebirth body = % x", body)
	}
}
