package wire

import (
	"bytes"
	"testing"
)

// Client 0x0086B14F..0x0086B265: a transformed player's spawn row carries
// flag 1 and the
// skin RefObj right after the avatar loop, then the gid block as before.
// A zero skin keeps the single 0 byte.
func TestPlayerSpawnRowCarriesTheTransformSkin(t *testing.T) {
	plain := testRow().Encode()
	row := testRow()
	row.Skin = TransformSkin{RefObjID: 1933}
	skinned := row.Encode()

	at := 4 + 2 + 2 + 2 // refobj, bind bytes, equip and avatar headers
	if plain[at] != 0 {
		t.Fatalf("plain row skin flag %d", plain[at])
	}
	want := concat(plain[:at], []byte{1}, u32le(1933), plain[at+1:])
	if !bytes.Equal(skinned, want) {
		t.Fatalf("skinned row:\n got %x\nwant %x", skinned, want)
	}
}

// The client's CPSMission_OnCharacterSkinChangeSuccess0x323A reads u32
// gid then u32 skin.
func TestSkinChangeBody(t *testing.T) {
	got := SkinChange{GID: 0x01020304, Skin: TransformSkin{RefObjID: 1933}}.Encode()
	if !bytes.Equal(got, concat(u32le(0x01020304), u32le(1933))) {
		t.Fatalf("0x323A body %x", got)
	}
	// The server's duplicate-skin fill: a player skin adds the record byte
	// and only the worn slots that hold an item.
	player := SkinChange{GID: 7, Skin: TransformSkin{RefObjID: 1907, Player: true, Shape: 4, Equipment: [9]uint32{0, 3643, 0, 0, 0, 0, 107}}}.Encode()
	if want := concat(u32le(7), u32le(1907), []byte{4, 2}, u32le(3643), u32le(107)); !bytes.Equal(player, want) {
		t.Fatalf("player skin %x, want %x", player, want)
	}
}

// The client's CSOItem_Deserialize reads them back: band 0x40 bodies are
// the RefObjID plus the group's
// own tail - the mask's monster, a pet summoner's record byte - never the
// 18-byte equipment body the reference server serializes.
func TestBand40ItemBodies(t *testing.T) {
	capsule := ItemBody{RefObjID: 10364, TypeFlags: PackTypeFlags(3, 2, 2, 0), TransformRefObjID: 1933}
	summoner := ItemBody{RefObjID: 2000, TypeFlags: PackTypeFlags(3, 2, 1, 1), Plus: 7, Durability: 9}
	for _, tc := range []struct {
		body ItemBody
		want []byte
	}{
		{capsule, concat(u32le(10364), u32le(1933))},
		{summoner, concat(u32le(2000), []byte{1})},
	} {
		got := tc.body.Encode()
		if !bytes.Equal(got, tc.want) || tc.body.EncodedSize() != len(tc.want) {
			t.Fatalf("%#x body %x (size %d), want %x", tc.body.TypeFlags, got, tc.body.EncodedSize(), tc.want)
		}
		back, err := readItemBody(NewReader(got), tc.body.TypeFlags)
		if err != nil || back.RefObjID != tc.body.RefObjID || back.TransformRefObjID != tc.body.TransformRefObjID {
			t.Fatalf("%#x read back %+v %v", tc.body.TypeFlags, back, err)
		}
	}
	if _, err := readItemBody(NewReader(concat(u32le(2000), []byte{2})), summoner.TypeFlags); err == nil {
		t.Fatal("a summoner with a pet record was accepted")
	}
}
