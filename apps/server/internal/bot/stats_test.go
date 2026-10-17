package bot_test

import (
	"testing"

	"opensro.online/server/internal/bot"
	"opensro.online/server/internal/game/item/wire"
)

// TestStatSpenderSpendsThePool proves the spend loop of issue #200: a level-up
// frame sets the pool, and the bot sends one STR request per point and never
// more requests than points.
func TestStatSpenderSpendsThePool(t *testing.T) {
	update, ok := bot.ParseExpUpdate(wire.EncodeExpUpdate(400001, 50, 5, true, 3))
	if !ok || !update.Levelled {
		t.Fatalf("update = %+v %v", update, ok)
	}
	spender := bot.NewStatSpender(bot.StatSTR)
	spender.SetPool(int(update.StatPoints))
	requests := 0
	for tick := 0; tick < 50; tick++ {
		opcode, send := spender.Next()
		if !send {
			continue
		}
		if opcode != wire.OpAllocStrRequest {
			t.Fatalf("opcode = 0x%04X, want the STR request", opcode)
		}
		requests++
		if spent, ok := spender.Ack(wire.EncodePointsAck(true, 0)); !spent || !ok {
			t.Fatalf("ack = %v %v", spent, ok)
		}
	}
	if requests != 3 || spender.Pool() != 0 {
		t.Fatalf("requests = %d pool = %d, want 3 and 0", requests, spender.Pool())
	}
}

// TestStatSpenderWaitsForTheAck guards the single request in flight: without
// an acknowledgement the spender sends the request again only after the wait.
func TestStatSpenderWaitsForTheAck(t *testing.T) {
	spender := bot.NewStatSpender(bot.StatINT)
	spender.SetPool(2)
	requests := 0
	for tick := 0; tick < 10; tick++ {
		if opcode, send := spender.Next(); send {
			if opcode != wire.OpAllocIntRequest {
				t.Fatalf("opcode = 0x%04X, want the INT request", opcode)
			}
			requests++
		}
	}
	if requests != 1 {
		t.Fatalf("requests in 10 ticks with no ack = %d, want 1", requests)
	}
	if _, send := spender.Next(); !send {
		t.Fatal("no second request after the wait")
	}
	if spender.Pool() != 2 {
		t.Fatalf("pool = %d, want 2: a request without an ack spends nothing", spender.Pool())
	}
}

// TestStatSpenderStopsOnARefusal guards the refusal: the server holds the real
// count, so a refusal empties the pool until the next absolute update.
func TestStatSpenderStopsOnARefusal(t *testing.T) {
	spender := bot.NewStatSpender(bot.StatSTR)
	spender.SetPool(5)
	if _, send := spender.Next(); !send {
		t.Fatal("no request")
	}
	if spent, ok := spender.Ack(wire.EncodePointsAck(false, 1)); spent || !ok {
		t.Fatalf("refusal ack = %v %v", spent, ok)
	}
	if _, send := spender.Next(); send || spender.Pool() != 0 {
		t.Fatalf("a refused spender sent again, pool = %d", spender.Pool())
	}
	if points, ok := bot.ParsePointsStat(wire.EncodePointsStatUpdate(4)); !ok || points != 4 {
		t.Fatalf("points update = %d %v", points, ok)
	}
	spender.SetPool(4)
	if _, send := spender.Next(); !send {
		t.Fatal("no request after a new absolute update")
	}
}

// TestStatSpenderNoneAndNil guards the off switch and the nil receiver.
func TestStatSpenderNoneAndNil(t *testing.T) {
	none := bot.NewStatSpender(bot.StatNone)
	none.SetPool(3)
	if _, send := none.Next(); send {
		t.Fatal("the none build sent a request")
	}
	var missing *bot.StatSpender
	missing.SetPool(3)
	if _, send := missing.Next(); send || missing.Pool() != 0 {
		t.Fatal("a nil spender sent a request")
	}
	if _, ok := missing.Ack([]byte{1}); ok {
		t.Fatal("a nil spender accepted an ack")
	}
	if _, ok := bot.ParsePointsStat([]byte{wire.PointsTypeSkill, 1, 0, 0, 0, 0}); ok {
		t.Fatal("a skill-point update was read as stat points")
	}
	for value, want := range map[string]bot.StatBuild{"str": bot.StatSTR, "INT": bot.StatINT, "none": bot.StatNone, "": bot.StatNone} {
		if got, err := bot.ParseStatBuild(value); err != nil || got != want {
			t.Fatalf("ParseStatBuild(%q) = %v %v", value, got, err)
		}
	}
	if _, err := bot.ParseStatBuild("dex"); err == nil {
		t.Fatal("an unknown build was accepted")
	}
}

// TestStatOpcodeAndName pins the build-to-opcode map and the canonical build
// names (issue #200): STR is 0x727A and INT is 0x7552, and a disabled build
// sends nothing.
func TestStatOpcodeAndName(t *testing.T) {
	if wire.OpAllocStrRequest != 0x727A || wire.OpAllocIntRequest != 0x7552 {
		t.Fatalf("stat opcodes = 0x%04X/0x%04X, want 0x727A/0x7552",
			wire.OpAllocStrRequest, wire.OpAllocIntRequest)
	}
	cases := []struct {
		build bot.StatBuild
		want  uint16
		name  string
	}{
		{bot.StatSTR, wire.OpAllocStrRequest, "str"},
		{bot.StatINT, wire.OpAllocIntRequest, "int"},
		{bot.StatNone, 0, "none"},
	}
	for _, tc := range cases {
		if got := bot.StatOpcode(tc.build); got != tc.want {
			t.Fatalf("StatOpcode(%v) = 0x%04X, want 0x%04X", tc.build, got, tc.want)
		}
		if got := bot.StatName(tc.build); got != tc.name {
			t.Fatalf("StatName(%v) = %q, want %q", tc.build, got, tc.name)
		}
	}
}
