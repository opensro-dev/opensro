package monster

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"
	"opensro.online/server/internal/testsupport/licensed"
	"os"
	"testing"
)

func TestNativeActorDistanceOriginalByteVectors(t *testing.T) {
	data, err := licensed.ServerFixture(t, "internal/game/world/monster/testdata/native-actor-distance.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		PortSHA256 string
		Vectors    []struct {
			FromPose, ToPose [4]float64
			DeltaBits        [3]uint32
			DistanceBits     uint32
		}
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	source, err := os.ReadFile("actor_distance.go")
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(source)
	if hex.EncodeToString(hash[:]) != fixture.PortSHA256 {
		t.Fatal("port changed after native experiment freeze")
	}
	if len(fixture.Vectors) != 2484 {
		t.Fatalf("incomplete corpus: %d", len(fixture.Vectors))
	}
	pose := func(v [4]float64) Pose { return Pose{RegionID: uint16(v[0]), X: v[1], Y: v[2], Z: v[3]} }
	for i, vector := range fixture.Vectors {
		from, to := pose(vector.FromPose), pose(vector.ToPose)
		x, y, z := NativeActorRelative(from, to)
		got := [3]uint32{math.Float32bits(x), math.Float32bits(y), math.Float32bits(z)}
		if got != vector.DeltaBits || math.Float32bits(NativeActorDistance(from, to)) != vector.DistanceBits {
			t.Fatalf("vector %d: delta %x want %x, distance %x want %x", i, got, vector.DeltaBits, math.Float32bits(NativeActorDistance(from, to)), vector.DistanceBits)
		}
	}
}
