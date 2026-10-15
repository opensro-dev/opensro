package bot_test

import (
	"bytes"
	"encoding/binary"
	"testing"

	"opensro.online/server/internal/bot"
)

// TestParseCharacterPosition guards the start position read from the 0x32B3
// char-data packet. The block is the object id, the region and the position. A
// false object-id match with the position 0,0 sits after the real block, so the
// parser must reject it and keep the real one.
func TestParseCharacterPosition(t *testing.T) {
	var body bytes.Buffer
	body.Write([]byte{0xde, 0xad}) // a leading block
	// The real block.
	binary.Write(&body, binary.LittleEndian, uint32(100009))
	binary.Write(&body, binary.LittleEndian, uint16(25256))
	binary.Write(&body, binary.LittleEndian, float32(960.5))
	binary.Write(&body, binary.LittleEndian, float32(-46.25))
	binary.Write(&body, binary.LittleEndian, float32(458.75))
	binary.Write(&body, binary.LittleEndian, uint16(0))
	// A false match: the object id, the region 1792 and the position 0,0.
	binary.Write(&body, binary.LittleEndian, uint32(100009))
	binary.Write(&body, binary.LittleEndian, uint16(1792))
	binary.Write(&body, binary.LittleEndian, float32(0))
	binary.Write(&body, binary.LittleEndian, float32(0))
	binary.Write(&body, binary.LittleEndian, float32(0))
	binary.Write(&body, binary.LittleEndian, uint16(0))

	region, x, y, z, ok := bot.ParseCharacterPosition(body.Bytes(), 100009, 25256)
	if !ok || region != 25256 || x != 960.5 || y != -46.25 || z != 458.75 {
		t.Fatalf("got region=%d x=%v y=%v z=%v ok=%v", region, x, y, z, ok)
	}
	// Without the region the 0,0 block is still rejected.
	region, x, y, z, ok = bot.ParseCharacterPosition(body.Bytes(), 100009, 0)
	if !ok || region != 25256 || x != 960.5 || y != -46.25 || z != 458.75 {
		t.Fatalf("got region=%d x=%v y=%v z=%v ok=%v", region, x, y, z, ok)
	}
	if _, _, _, _, ok := bot.ParseCharacterPosition(body.Bytes(), 7, 0); ok {
		t.Fatal("a foreign gid matched")
	}
}
