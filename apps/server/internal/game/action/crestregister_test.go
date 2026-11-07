/*
===========================================================================

crestregister_test.go - a crest register upload stands the crest

#2282 (register half): the master's picked image rides control
opcode 19 to the crest store, which re-encodes it and stands the new
revision through the guild or union door; the item is spent and the
0xB5BD success answers with the 0x34F3 update behind it. A refusal -
non-master, standing crest, bad image, rate limit - writes nothing
and spends nothing. These tests drive the real guild store door,
the real union authority and a real crest store on disk.

===========================================================================
*/
package action

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"os"
	"testing"
	"time"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/social/crest"
	"opensro.online/server/internal/game/social/union"
)

/*
================
crestRegisterFixtureWith

The shared crest-item fixture holding the register item of the named
kind (param2 1): the guild item (9268) or the union item (9270).
================
*/
func crestRegisterFixtureWith(t *testing.T, codename string, refObjID uint32, param1 float64, opts crestDeleteOptions) (*doorRuntime, *enterworld.Character, int64) {
	t.Helper()
	return crestItemFixtureWith(t, codename, refObjID, param1, 1, opts)
}

/*
================
armCrestStore

Give the runtime a real crest store in a scratch directory and a
capture of every audited change. Returns the store, its directory
(for "nothing was written" assertions) and the changes.
================
*/
func armCrestStore(t *testing.T, d *doorRuntime) (*crest.Store, string, *[]CrestChange) {
	t.Helper()
	dir := t.TempDir()
	store, err := crest.NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	d.rt.Crests = store
	changes := &[]CrestChange{}
	d.rt.RecordCrestChange = func(change CrestChange) { *changes = append(*changes, change) }
	return store, dir, changes
}

/*
================
crestTestPNG

A small solid PNG the re-encode accepts.
================
*/
func crestTestPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 32, 32))
	for y := 0; y < 32; y++ {
		for x := 0; x < 32; x++ {
			img.SetRGBA(x, y, color.RGBA{0xC0, 0x10, 0x10, 0xFF})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

/*
================
uploadCrest

One opcode-19 upload of the image from the bag-slot-21 register item.
================
*/
func uploadCrest(t *testing.T, d *doorRuntime, c *enterworld.Character, kind string, image []byte) OpResult {
	t.Helper()
	request, err := json.Marshal(crestRegisterRequest{Kind: kind, Slot: 21,
		Image: base64.StdEncoding.EncodeToString(image)})
	if err != nil {
		t.Fatal(err)
	}
	return d.rt.HandleCrestRegister(testDivision, c, request)
}

/*
================
crestAnswerOf

The upload answer an operation returned; every call answers.
================
*/
func crestAnswerOf(t *testing.T, result OpResult) crestRegisterAnswer {
	t.Helper()
	for _, frame := range result.Frames {
		if frame.Opcode != opCrestRegister {
			continue
		}
		var answer crestRegisterAnswer
		if err := json.Unmarshal(frame.Payload, &answer); err != nil {
			t.Fatalf("upload answer does not decode: %v", err)
		}
		return answer
	}
	t.Fatalf("no upload answer in %+v", result)
	return crestRegisterAnswer{}
}

/*
================
assertCrestUploadRefused

A refused upload carries the named answer, spends nothing, and
publishes no crest update.
================
*/
func assertCrestUploadRefused(t *testing.T, result OpResult, holder *enterworld.Character, reason string, code uint8) {
	t.Helper()
	answer := crestAnswerOf(t, result)
	if answer.OK || answer.Reason != reason || answer.Code != code {
		t.Fatalf("upload answer = %+v, want refusal %q code %#x", answer, reason, code)
	}
	if row, ok := inventoryRowAt(holder, 21); !ok || row.StackCount != 1 {
		t.Fatal("refused upload spent the item")
	}
	if payload := crestUpdateFrame(result); payload != nil {
		t.Fatalf("refused upload published % X", payload)
	}
}

/*
================
assertCrestStoreEmpty

Nothing was written for a refusal: no payload file, no index.
================
*/
func assertCrestStoreEmpty(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		names := []string{}
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		t.Fatalf("crest store holds %v after a refusal, want nothing written", names)
	}
}

/*
================
TestGuildCrestRegisterStandsTheCrest
================
*/
func TestGuildCrestRegisterStandsTheCrest(t *testing.T) {
	d, c, guildID := crestRegisterFixtureWith(t, "ITEM_ETC_GUILD_CREST_USER", 9268, 1, crestDeleteOptions{})
	store, _, changes := armCrestStore(t, d)
	image := crestTestPNG(t)

	result := uploadCrest(t, d, c, crest.KindGuild, image)
	answer := crestAnswerOf(t, result)
	if !answer.OK || answer.Crest != 1 {
		t.Fatalf("upload answer = %+v, want ok at revision 1", answer)
	}
	// The burst: the lane's ok, the ordinary item-use success, then
	// the crest update naming revision 1.
	if len(result.Frames) < 3 || result.Frames[1].Opcode != wire.OpItemUseResponse || result.Frames[1].Payload[0] != 1 {
		t.Fatalf("register burst = %+v, want the item-use success behind the answer", result.Frames)
	}
	payload := crestUpdateFrame(result)
	if len(payload) < 7 || payload[0] != 1 || binary.LittleEndian.Uint32(payload[len(payload)-4:]) != 1 {
		t.Fatalf("crest update = % X, want a guild row at revision 1", payload)
	}
	if _, ok := inventoryRowAt(c, 21); ok {
		t.Fatal("register item not spent")
	}
	if record, _, ok := d.authority.Guilds().Guild(testDivision, guildID); !ok || record.CrestParam != 1 {
		t.Fatalf("stored crest = %d (ok %t), want 1", record.CrestParam, ok)
	}
	// The store holds exactly the canonical re-encode under its hash.
	want, err := crest.Reencode(image)
	if err != nil {
		t.Fatal(err)
	}
	ref, ok := store.Current(crest.KindGuild, guildID)
	if !ok || ref.Revision != 1 {
		t.Fatalf("standing crest = %+v (%t), want revision 1", ref, ok)
	}
	served, hash, ok := store.ServeCrest(crest.KindGuild, guildID, 1)
	if !ok || hash != ref.Hash || !bytes.Equal(served, want) {
		t.Fatal("the stored payload is not the canonical re-encode")
	}
	// The audit row: character, guild, old and new hash.
	if len(*changes) != 1 {
		t.Fatalf("audit changes = %+v, want one", *changes)
	}
	change := (*changes)[0]
	if change.Character != c.Name || change.Kind != crest.KindGuild || change.GuildID != guildID ||
		change.NewRevision != 1 || change.NewHash != ref.Hash || change.OldHash != "" || change.Source != "upload" {
		t.Fatalf("audit change = %+v", change)
	}
}

/*
================
TestGuildCrestRegisterNonMasterRefused
================
*/
func TestGuildCrestRegisterNonMasterRefused(t *testing.T) {
	d, member, guildID := crestRegisterFixtureWith(t, "ITEM_ETC_GUILD_CREST_USER", 9268, 1,
		crestDeleteOptions{asMember: true})
	_, dir, changes := armCrestStore(t, d)

	result := uploadCrest(t, d, member, crest.KindGuild, crestTestPNG(t))
	assertCrestUploadRefused(t, result, member, crestRefusedGeneric, wire.ErrCodeCrestNotMaster)
	assertCrestStoreEmpty(t, dir)
	if record, _, ok := d.authority.Guilds().Guild(testDivision, guildID); !ok || record.CrestParam != 0 {
		t.Fatal("a non-master's upload stood a crest")
	}
	if len(*changes) != 0 {
		t.Fatalf("a refused upload was audited: %+v", *changes)
	}
}

/*
================
TestGuildCrestRegisterLevelGateRefused
================
*/
func TestGuildCrestRegisterLevelGateRefused(t *testing.T) {
	d, c, guildID := crestRegisterFixtureWith(t, "ITEM_ETC_GUILD_CREST_USER", 9268, 1,
		crestDeleteOptions{guildLevel: 3})
	_, dir, _ := armCrestStore(t, d)

	result := uploadCrest(t, d, c, crest.KindGuild, crestTestPNG(t))
	assertCrestUploadRefused(t, result, c, crestRefusedGeneric, wire.ErrCodeCrestGuildLevelTooLow)
	assertCrestStoreEmpty(t, dir)
	if record, _, ok := d.authority.Guilds().Guild(testDivision, guildID); !ok || record.CrestParam != 0 {
		t.Fatal("a low guild's upload stood a crest")
	}
}

/*
================
TestGuildCrestRegisterOverlapRefused

A crest already standing is the native OVERLAP refusal: nothing is
written, the item is kept, and the standing revision does not move.
================
*/
func TestGuildCrestRegisterOverlapRefused(t *testing.T) {
	d, c, guildID := crestRegisterFixtureWith(t, "ITEM_ETC_GUILD_CREST_USER", 9268, 1, crestDeleteOptions{})
	_, dir, changes := armCrestStore(t, d)
	setGuildCrest(t, d, c, 4242)

	result := uploadCrest(t, d, c, crest.KindGuild, crestTestPNG(t))
	assertCrestUploadRefused(t, result, c, crestRefusedOverlap, 0)
	assertCrestStoreEmpty(t, dir)
	if record, _, ok := d.authority.Guilds().Guild(testDivision, guildID); !ok || record.CrestParam != 4242 {
		t.Fatal("an overlap refusal moved the standing crest")
	}
	if len(*changes) != 0 {
		t.Fatalf("an overlap refusal was audited: %+v", *changes)
	}
}

/*
================
TestGuildCrestRegisterBadImageRefused

Every unusable image is the native ERROR answer and writes nothing:
a renamed text file, a BMP-magic forgery, a truncated PNG, an empty
body.
================
*/
func TestGuildCrestRegisterBadImageRefused(t *testing.T) {
	pngBytes := crestTestPNG(t)
	images := map[string][]byte{
		"renamed text":   []byte("this is not an image at all"),
		"bmp forgery":    {0x42, 0x4D, 0x01, 0x02, 0x03},
		"truncated png":  pngBytes[:len(pngBytes)/2],
		"empty":          {},
		"polyglot image": append(append([]byte{}, pngBytes...), []byte("<script>alert(1)</script>")...),
	}
	for name, image := range images {
		d, c, _ := crestRegisterFixtureWith(t, "ITEM_ETC_GUILD_CREST_USER", 9268, 1, crestDeleteOptions{})
		_, dir, _ := armCrestStore(t, d)
		result := uploadCrest(t, d, c, crest.KindGuild, image)
		answer := crestAnswerOf(t, result)
		if answer.OK || answer.Reason != crestRefusedInvalid {
			t.Fatalf("%s: upload answer = %+v, want the invalid refusal", name, answer)
		}
		if row, ok := inventoryRowAt(c, 21); !ok || row.StackCount != 1 {
			t.Fatalf("%s: a refused upload spent the item", name)
		}
		assertCrestStoreEmpty(t, dir)
	}
}

/*
================
TestGuildCrestRegisterRateLimit

One change per cooldown: after a register and a clear, an immediate
re-register refuses until the cooldown passes - and then it takes
the next revision, never the cleared one again.
================
*/
func TestGuildCrestRegisterRateLimit(t *testing.T) {
	d, c, guildID := crestRegisterFixtureWith(t, "ITEM_ETC_GUILD_CREST_USER", 9268, 1, crestDeleteOptions{})
	store, _, _ := armCrestStore(t, d)
	image := crestTestPNG(t)

	if answer := crestAnswerOf(t, uploadCrest(t, d, c, crest.KindGuild, image)); !answer.OK {
		t.Fatalf("first upload = %+v", answer)
	}
	// A fresh register item (the first was spent) and a cleared crest.
	d.authority.UpdateCharacter(c, "crest-register-fixture", func() bool {
		c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{Slot: 21,
			RefObjID: 9268, Codename: "ITEM_ETC_GUILD_CREST_USER",
			TypeFlags: d.rt.deps.ItemReferences().(staticItemSource)["ITEM_ETC_GUILD_CREST_USER"].TypeFlags(), StackCount: 1})
		return true
	})
	setGuildCrest(t, d, c, 0)
	if err := store.RemoveCurrent(crest.KindGuild, guildID); err != nil {
		t.Fatal(err)
	}

	result := uploadCrest(t, d, c, crest.KindGuild, image)
	assertCrestUploadRefused(t, result, c, crestRefusedGeneric, 0)

	d.clock.Advance(crest.ChangeCooldown + time.Minute)
	result = uploadCrest(t, d, c, crest.KindGuild, image)
	if answer := crestAnswerOf(t, result); !answer.OK || answer.Crest != 2 {
		t.Fatalf("post-cooldown upload = %+v, want ok at revision 2", answer)
	}
	if record, _, ok := d.authority.Guilds().Guild(testDivision, guildID); !ok || record.CrestParam != 2 {
		t.Fatalf("stored crest = %d (ok %t), want 2", record.CrestParam, ok)
	}
}

/*
================
TestUnionCrestRegisterStandsTheCrest
================
*/
func TestUnionCrestRegisterStandsTheCrest(t *testing.T) {
	d, c, guildID := crestRegisterFixtureWith(t, "ITEM_ETC_UNION_CREST_USER", 9270, 2,
		crestDeleteOptions{union: true})
	store, _, changes := armCrestStore(t, d)
	// The seeded union carries a crest; the delete half clears it, so
	// the register lane starts from none standing.
	if _, refusal, err := d.rt.Unions.ClearCrest(testDivision, guildID); err != nil || refusal != union.CrestRefusalNone {
		t.Fatalf("seeded union clear: %v %v", refusal, err)
	}

	result := uploadCrest(t, d, c, crest.KindUnion, crestTestPNG(t))
	if answer := crestAnswerOf(t, result); !answer.OK || answer.Crest != 1 {
		t.Fatalf("union upload answer = %+v, want ok at revision 1", answer)
	}
	if _, ok := inventoryRowAt(c, 21); ok {
		t.Fatal("union register item not spent")
	}
	stored, err := d.authority.Alliances().Alliances(testDivision)
	if err != nil || len(stored) != 1 || stored[0].Crest != 1 {
		t.Fatalf("stored union = %+v (%v), want crest 1", stored, err)
	}
	payload := crestUpdateFrame(result)
	if len(payload) < 6 || payload[0] != 2 || binary.LittleEndian.Uint32(payload[len(payload)-4:]) != 1 {
		t.Fatalf("crest update = % X, want a union row at revision 1", payload)
	}
	if ref, ok := store.Current(crest.KindUnion, guildID); !ok || ref.Revision != 1 {
		t.Fatalf("standing union crest = %+v (%t), want revision 1", ref, ok)
	}
	if len(*changes) != 1 || (*changes)[0].Kind != crest.KindUnion || (*changes)[0].AllianceID != guildID {
		t.Fatalf("audit changes = %+v, want the union's row", *changes)
	}
}

/*
================
TestUnionCrestRegisterLedByOtherRefused

A guild that merely stands in a union cannot crest it: the master's
guild gate (0xBC), the seeded crest untouched, nothing written.
================
*/
func TestUnionCrestRegisterLedByOtherRefused(t *testing.T) {
	d, c, _ := crestRegisterFixtureWith(t, "ITEM_ETC_UNION_CREST_USER", 9270, 2,
		crestDeleteOptions{unionLedByOther: true})
	_, dir, _ := armCrestStore(t, d)

	result := uploadCrest(t, d, c, crest.KindUnion, crestTestPNG(t))
	assertCrestUploadRefused(t, result, c, crestRefusedGeneric, wire.ErrCodeCrestNotMaster)
	assertCrestStoreEmpty(t, dir)
	stored, err := d.authority.Alliances().Alliances(testDivision)
	if err != nil || len(stored) != 1 || stored[0].Crest != 777 {
		t.Fatalf("stored union = %+v (%v), want the seeded crest untouched", stored, err)
	}
}

/*
================
TestUnionCrestRegisterWithoutUnionRefused
================
*/
func TestUnionCrestRegisterWithoutUnionRefused(t *testing.T) {
	d, c, _ := crestRegisterFixtureWith(t, "ITEM_ETC_UNION_CREST_USER", 9270, 2, crestDeleteOptions{})
	_, dir, _ := armCrestStore(t, d)
	d.rt.Unions = union.New()
	if err := d.rt.Unions.Restore(testDivision, d.authority.Alliances()); err != nil {
		t.Fatal(err)
	}

	result := uploadCrest(t, d, c, crest.KindUnion, crestTestPNG(t))
	assertCrestUploadRefused(t, result, c, crestRefusedGeneric, wire.ErrCodeCrestNoUnion)
	assertCrestStoreEmpty(t, dir)
}

/*
================
TestCrestRegisterUploadNeedsTheRightItem

The lane judges the item, not the claim: the delete item at the
slot, a register item of the other kind, and an empty slot all
refuse as the generic failure and write nothing.
================
*/
func TestCrestRegisterUploadNeedsTheRightItem(t *testing.T) {
	// The delete item (param2 2) is not a register item.
	d, c, _ := crestDeleteFixture(t, "ITEM_ETC_GUILD_CREST_DELETE", 1)
	_, dir, _ := armCrestStore(t, d)
	result := uploadCrest(t, d, c, crest.KindGuild, crestTestPNG(t))
	assertCrestUploadRefused(t, result, c, crestRefusedGeneric, 0)
	assertCrestStoreEmpty(t, dir)

	// The union register item cannot stand a guild crest.
	d, c, _ = crestRegisterFixtureWith(t, "ITEM_ETC_UNION_CREST_USER", 9270, 2, crestDeleteOptions{})
	_, dir, _ = armCrestStore(t, d)
	result = uploadCrest(t, d, c, crest.KindGuild, crestTestPNG(t))
	assertCrestUploadRefused(t, result, c, crestRefusedGeneric, 0)
	assertCrestStoreEmpty(t, dir)

	// No item at the slot at all.
	d, c, _ = crestRegisterFixtureWith(t, "ITEM_ETC_GUILD_CREST_USER", 9268, 1, crestDeleteOptions{})
	_, dir, _ = armCrestStore(t, d)
	request, err := json.Marshal(crestRegisterRequest{Kind: crest.KindGuild, Slot: 30,
		Image: base64.StdEncoding.EncodeToString(crestTestPNG(t))})
	if err != nil {
		t.Fatal(err)
	}
	result = d.rt.HandleCrestRegister(testDivision, c, request)
	answer := crestAnswerOf(t, result)
	if answer.OK || answer.Reason != crestRefusedGeneric {
		t.Fatalf("empty-slot upload = %+v, want the generic refusal", answer)
	}
	assertCrestStoreEmpty(t, dir)
}

/*
================
TestCrestStaffRemoval

The operator removal (#2282): the guild crest reverts to none with
an operator audit row naming the hash it removed; a union removal
clears from any member's standing; and each refusal is an error the
console can show.
================
*/
func TestCrestStaffRemoval(t *testing.T) {
	d, c, guildID := crestRegisterFixtureWith(t, "ITEM_ETC_GUILD_CREST_USER", 9268, 1, crestDeleteOptions{})
	store, _, changes := armCrestStore(t, d)

	if answer := crestAnswerOf(t, uploadCrest(t, d, c, crest.KindGuild, crestTestPNG(t))); !answer.OK {
		t.Fatalf("upload = %+v", answer)
	}
	standing, _ := store.Current(crest.KindGuild, guildID)

	if err := d.rt.OperatorRemoveCrest(testDivision, "console-op", c.Name, "bogus"); err == nil {
		t.Fatal("a removal naming neither crest was accepted")
	}
	if err := d.rt.OperatorRemoveCrest(testDivision, "console-op", "Nobody", crest.KindGuild); err == nil {
		t.Fatal("a removal for an absent character was accepted")
	}
	if err := d.rt.OperatorRemoveCrest(testDivision, "console-op", c.Name, crest.KindGuild); err != nil {
		t.Fatalf("staff removal: %v", err)
	}
	if record, _, ok := d.authority.Guilds().Guild(testDivision, guildID); !ok || record.CrestParam != 0 {
		t.Fatal("the staff removal left the crest standing")
	}
	if _, ok := store.Current(crest.KindGuild, guildID); ok {
		t.Fatal("the staff removal left the crest indexed")
	}
	if err := d.rt.OperatorRemoveCrest(testDivision, "console-op", c.Name, crest.KindGuild); err == nil {
		t.Fatal("a second removal of nothing was accepted")
	}
	if len(*changes) != 2 {
		t.Fatalf("audit changes = %+v, want upload + removal", *changes)
	}
	removal := (*changes)[1]
	if removal.Source != "operator" || removal.Operator != "console-op" || removal.Kind != crest.KindGuild ||
		removal.OldHash != standing.Hash || removal.OldRevision != 1 || removal.NewRevision != 0 {
		t.Fatalf("removal audit = %+v", removal)
	}
}

/*
================
TestUnionCrestStaffRemoval

The staff union removal clears the crest of the union the named
character's guild merely stands in - leadership is the upload
lane's rule, not the operator's.
================
*/
func TestUnionCrestStaffRemoval(t *testing.T) {
	d, c, guildID := crestRegisterFixtureWith(t, "ITEM_ETC_UNION_CREST_USER", 9270, 2,
		crestDeleteOptions{unionLedByOther: true})
	store, _, changes := armCrestStore(t, d)
	record, _ := d.rt.Unions.CrestState(testDivision, guildID)
	if err := store.SetCurrent(crest.KindUnion, record.AllianceID, crest.Ref{Revision: 777, Hash: "seeded"}); err != nil {
		t.Fatal(err)
	}

	if err := d.rt.OperatorRemoveCrest(testDivision, "console-op", c.Name, crest.KindUnion); err != nil {
		t.Fatalf("staff union removal: %v", err)
	}
	stored, err := d.authority.Alliances().Alliances(testDivision)
	if err != nil || len(stored) != 1 || stored[0].Crest != 0 {
		t.Fatalf("stored union = %+v (%v), want crest cleared", stored, err)
	}
	if _, ok := store.Current(crest.KindUnion, record.AllianceID); ok {
		t.Fatal("the staff removal left the union crest indexed")
	}
	if len(*changes) != 1 || (*changes)[0].Source != "operator" || (*changes)[0].Kind != crest.KindUnion ||
		(*changes)[0].OldHash != "seeded" || (*changes)[0].AllianceID != record.AllianceID {
		t.Fatalf("audit changes = %+v, want the operator's union removal", *changes)
	}
}
