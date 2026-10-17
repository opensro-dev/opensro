package bot_test

import (
	"encoding/binary"
	"testing"

	"opensro.online/server/internal/bot"
)

func TestParseVitals(t *testing.T) {
	body := make([]byte, 15)
	binary.LittleEndian.PutUint32(body[0:4], 7)
	body[6] = 0x03
	binary.LittleEndian.PutUint32(body[7:11], 123)
	binary.LittleEndian.PutUint32(body[11:15], 45)

	v, ok := bot.ParseVitals(body)
	if !ok || v.GID != 7 || v.HP != 123 || v.MP != 45 {
		t.Fatalf("vitals = %+v ok=%v", v, ok)
	}
	if _, ok := bot.ParseVitals(body[:10]); ok {
		t.Fatal("a short vitals body was accepted")
	}
}

func TestEncodeItemUse(t *testing.T) {
	body := bot.EncodeItemUse(15, 2284)
	if len(body) != 3 || body[0] != 15 || binary.LittleEndian.Uint16(body[1:3]) != 2284 {
		t.Fatalf("item use body = % x", body)
	}
}
