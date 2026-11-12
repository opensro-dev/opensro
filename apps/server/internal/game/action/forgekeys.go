/*
===========================================================================

forgekeys.go - the /FORGE key table: which keys an item accepts, and why

Port-only, not native (issue #1980; forge.go owns the command). The keys
are DERIVED, never hand-listed per item: the item reference picks the
families (combat.WhiteStatFields names the white variance fields, the
alchemy catalogue names the magic options the item's degree and body
can carry, the stack/COS/capsule predicates name the rest), and this
one table drives both validation and the /forge help lines, so what
help lists is exactly what the forge accepts.

Value rules mirror the lanes that would have produced the value: a
blue value is one a stone could land (alchemy/stones.go: counted tags
stack 1..6, 'rep' 1..7, generated values clamp at 1700; an avatar's
options roll their single range instead, avatar.go), white percents
become 5-bit fields, and the blue count may not exceed the capacity
Catalog.MagicLimit grants the forged set itself.

===========================================================================
*/
package action

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/alchemy"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
)

// The forge's own bounds. Plus rides the item body's u8 (255 is the
// body cap; the owner ruled plus uncapped below alchemy's allowance,
// and the body is the ceiling). A pet's level rides CharacterCOS's u8
// against the shard's 140 level cap.
const (
	forgePlusMax         = 255
	forgeWhitePercentMax = 100
	forgePetLevelMax     = 140
	forgePetNameMaxBytes = 16
	// forgeBlueValueCap is alchemy's generated-value clamp (stones.go
	// and avatar.go agree on 1700 = 0x6A4).
	forgeBlueValueCap = 1700
)

// Magic option tags (magicoption.txt's numeric tag column, the
// big-endian fold of combat's optionTag), named for the counted stacks
// and the durability pair that raises an item's blue capacity.
const (
	forgeTagAtha   = 0x61746861 // 'atha'
	forgeTagLuck   = 0x6c75636b // 'luck'
	forgeTagSoli   = 0x736f6c69 // 'soli'
	forgeTagAstral = 0x61737472 // 'astr'
	forgeTagApe    = 0x00617065 // 'ape'
	forgeTagRepair = 0x00726570 // 'rep'
	forgeTagDura   = 0x64757261 // 'dura'
	forgeTagNrep   = 0x6e726570 // 'nrep'
)

type forgeKeyKind uint8

const (
	forgeKeyBound forgeKeyKind = iota
	forgeKeyPlus
	forgeKeyWhiteAll
	forgeKeyWhiteField
	forgeKeyBlue
	forgeKeyBlueFull
	forgeKeyDurability
	forgeKeyCount
	forgeKeyTransform
	forgeKeyPetLevel
	forgeKeyPetName
)

// forgeKey is one accepted key: its typed name, the help line that
// describes it, and everything validation needs to judge a value.
type forgeKey struct {
	name   string
	help   string
	kind   forgeKeyKind
	field  uint           // white.<stat>: the variance field index
	option alchemy.Magic  // blue.<OPTION>: the catalogue row
	rng    forgeBlueRange // blue.<OPTION>: the legal values
	lo, hi uint32         // scalar keys: the inclusive value range
}

// forgeBlueRange is the legal value set of one magic option: either a
// counted stack (1..cap), a single rollable range, or the discrete set
// a magic stone can land.
type forgeBlueRange struct {
	counted bool
	lo, hi  uint32
	values  []uint32
}

/*
================
forgeBlueRange.maxValue
================
*/
func (r forgeBlueRange) maxValue() uint32 {
	if len(r.values) > 0 {
		return r.values[len(r.values)-1]
	}
	return r.hi
}

/*
================
forgeBlueRange.accepts
================
*/
func (r forgeBlueRange) accepts(value uint32) bool {
	if len(r.values) > 0 {
		for _, legal := range r.values {
			if value == legal {
				return true
			}
		}
		return false
	}
	return value >= r.lo && value <= r.hi
}

/*
================
forgeBlueRange.describe
================
*/
func (r forgeBlueRange) describe() string {
	if len(r.values) > 0 {
		parts := make([]string, len(r.values))
		for i, value := range r.values {
			parts[i] = strconv.FormatUint(uint64(value), 10)
		}
		return "one of " + strings.Join(parts, "|")
	}
	return fmt.Sprintf("%d..%d", r.lo, r.hi)
}

/*
================
forgeCountedCap

The counted-tag ceilings: the protection stacks and the repair count,
as stones.go (and, minus 'ape', avatar.go) cap them.
================
*/
func forgeCountedCap(tag uint32, avatar bool) (uint32, bool) {
	switch tag {
	case forgeTagAtha, forgeTagLuck, forgeTagSoli, forgeTagAstral:
		return 6, true
	case forgeTagApe:
		if !avatar {
			return 6, true
		}
	case forgeTagRepair:
		return 7, true
	}
	return 0, false
}

/*
================
forgeBlueRangeFor

The legal values of one option. Equipment options take the discrete
set RollMagicValue draws from (the nonzero u16 halves of the row's
three params), each under the 1700 clamp a landed stone applies; an
avatar's options roll RollSingleRange's range instead. An option with
no legal value is not offered at all.
================
*/
func forgeBlueRangeFor(option alchemy.Magic, avatar bool) (forgeBlueRange, bool) {
	if cap, counted := forgeCountedCap(option.Tag, avatar); counted {
		return forgeBlueRange{counted: true, lo: 1, hi: cap}, true
	}
	if avatar {
		lo := max(option.Params[1], 1)
		hi := min(option.Params[2], forgeBlueValueCap)
		if hi < lo {
			hi = lo
		}
		if option.Params[1] == 0 && option.Params[2] == 0 {
			return forgeBlueRange{}, false
		}
		return forgeBlueRange{lo: lo, hi: hi}, true
	}
	set := map[uint32]bool{}
	for _, param := range option.Params {
		for _, value := range []uint32{param >> 16, param & 0xffff} {
			if value > 0 {
				set[min(value, forgeBlueValueCap)] = true
			}
		}
	}
	if len(set) == 0 {
		return forgeBlueRange{}, false
	}
	values := make([]uint32, 0, len(set))
	for value := range set {
		values = append(values, value)
	}
	sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
	return forgeBlueRange{values: values}, true
}

/*
================
forgeBlueKeys

The blue.<OPTION> keys of one item: every catalogue option at the
item's degree the item may carry - Magic.Allows for equipment, the
magicoptionassign part list for an avatar - in param-id order.
================
*/
func forgeBlueKeys(ref *enterworld.ItemRef, cat *alchemy.Catalog) []forgeKey {
	if cat == nil {
		return nil
	}
	catRef, ok := cat.Items[ref.Codename]
	if !ok {
		return nil
	}
	degree := catRef.Degree()
	part, avatar := alchemy.AvatarPart(ref.TypeFlags())
	ids := make([]uint16, 0, len(cat.Magic))
	for id := range cat.Magic {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	seen := map[string]bool{}
	var keys []forgeKey
	for _, id := range ids {
		option := cat.Magic[id]
		if option.Degree != degree || seen[option.Name] {
			continue
		}
		if avatar {
			assigned := false
			for _, name := range cat.AvatarOptions[part] {
				if name == option.Name {
					assigned = true
					break
				}
			}
			if !assigned {
				continue
			}
		} else if !option.Allows(ref.TypeFlags()) {
			continue
		}
		rng, legal := forgeBlueRangeFor(option, avatar)
		if !legal {
			continue
		}
		seen[option.Name] = true
		keys = append(keys, forgeKey{
			name:   "blue." + strings.ToLower(option.Name),
			help:   fmt.Sprintf("blue.%s %s or max (degree %d)", option.Name, rng.describe(), degree),
			kind:   forgeKeyBlue,
			option: option,
			rng:    rng,
		})
	}
	return keys
}

/*
================
forgeKeysFor

The whole key table of one item, in help order. maxStack is the
caller's stack cap for the item (Runtime.maxStackFor); the durability
ceiling is deliberately not a constant here - assemble judges it
against the forged item's own final spec.
================
*/
func forgeKeysFor(ref *enterworld.ItemRef, cat *alchemy.Catalog, maxStack uint16) []forgeKey {
	keys := []forgeKey{{
		name: "bound",
		help: "bound (always on; bound=0 refuses)",
		kind: forgeKeyBound,
	}}
	whiteFields := combat.WhiteStatFields(ref.TypeFlags())
	if len(whiteFields) > 0 {
		keys = append(keys, forgeKey{
			name: "plus",
			help: fmt.Sprintf("plus 0..%d", forgePlusMax),
			kind: forgeKeyPlus, lo: 0, hi: forgePlusMax,
		})
		keys = append(keys, forgeKey{
			name: "white",
			help: fmt.Sprintf("white 0..%d%% (every white stat)", forgeWhitePercentMax),
			kind: forgeKeyWhiteAll, lo: 0, hi: forgeWhitePercentMax,
		})
		for _, field := range whiteFields {
			keys = append(keys, forgeKey{
				name:  "white." + field.Name,
				help:  fmt.Sprintf("white.%s 0..%d%%", field.Name, forgeWhitePercentMax),
				kind:  forgeKeyWhiteField,
				field: field.Field, lo: 0, hi: forgeWhitePercentMax,
			})
		}
	}
	blues := forgeBlueKeys(ref, cat)
	if len(blues) > 0 {
		keys = append(keys, forgeKey{
			name: "blue",
			help: "blue=full (every option below at max)",
			kind: forgeKeyBlueFull,
		})
		keys = append(keys, blues...)
	}
	if len(whiteFields) > 0 && whiteFields[0].Name == "dur" {
		keys = append(keys, forgeKey{
			name: "durability",
			help: "durability <points> (max = full at the forged spec)",
			kind: forgeKeyDurability, lo: 1, hi: math.MaxUint32,
		})
	}
	if inventory.IsEtcStackableTypeFlags(ref.TypeFlags()) && maxStack > 1 {
		keys = append(keys, forgeKey{
			name: "count",
			help: fmt.Sprintf("count 1..%d", maxStack),
			kind: forgeKeyCount, lo: 1, hi: uint32(maxStack),
		})
	}
	if wire.IsMonsterCapsule(ref.TypeFlags()) {
		keys = append(keys, forgeKey{
			name: "transform",
			help: "transform <monster codename>",
			kind: forgeKeyTransform,
		})
	}
	if wire.IsCosSummoner(ref.TypeFlags()) && ref.AssociatedCharacterCodename != "" {
		keys = append(keys, forgeKey{
			name: "pet.level",
			help: fmt.Sprintf("pet.level 1..%d", forgePetLevelMax),
			kind: forgeKeyPetLevel, lo: 1, hi: forgePetLevelMax,
		}, forgeKey{
			name: "pet.name",
			help: fmt.Sprintf("pet.name <name, at most %d bytes>", forgePetNameMaxBytes),
			kind: forgeKeyPetName,
		})
	}
	return keys
}

/*
================
forgeHelpLines

The /forge help answer for one item: a header naming the item and its
degree, then one line per key forgeKeysFor accepts.
================
*/
func forgeHelpLines(ref *enterworld.ItemRef, cat *alchemy.Catalog, maxStack uint16) []string {
	header := "forge " + ref.Codename + ": keys"
	if cat != nil {
		if catRef, ok := cat.Items[ref.Codename]; ok {
			header += fmt.Sprintf(" (degree %d, blue capacity %d)", catRef.Degree(), catRef.MaxMagic)
		}
	}
	lines := []string{header}
	for _, key := range forgeKeysFor(ref, cat, maxStack) {
		lines = append(lines, key.help)
	}
	return lines
}

// forgeSpec is one parsed /forge command: every value the GM named,
// still unapplied. Minus-one scalars read as unset.
type forgeSpec struct {
	plus         int
	whiteAll     int
	whiteFields  map[uint]int
	blues        []forgeBlueGrant
	blueFull     bool
	durability   int64
	count        int
	transformRef uint32
	transformSet bool
	petLevel     int
	petName      string
	petNameSet   bool
}

// forgeBlueGrant is one chosen option at one chosen value.
type forgeBlueGrant struct {
	option alchemy.Magic
	value  uint32
}

/*
================
newForgeSpec
================
*/
func newForgeSpec() forgeSpec {
	return forgeSpec{
		plus: -1, whiteAll: -1, whiteFields: map[uint]int{},
		durability: -1, count: -1, petLevel: -1,
	}
}

// forgeBuilder carries one command's working state: the item
// reference, the derived key table and the spec being filled.
type forgeBuilder struct {
	rt     *Runtime
	ref    *enterworld.ItemRef
	cat    *alchemy.Catalog
	keys   map[string]forgeKey
	spec   forgeSpec
	seen   map[string]bool
	blueBy map[uint16]bool
}

/*
================
newForgeBuilder
================
*/
func (rt *Runtime) newForgeBuilder(ref *enterworld.ItemRef) *forgeBuilder {
	builder := &forgeBuilder{
		rt: rt, ref: ref, cat: rt.Alchemy,
		keys: map[string]forgeKey{}, spec: newForgeSpec(),
		seen: map[string]bool{}, blueBy: map[uint16]bool{},
	}
	for _, key := range forgeKeysFor(ref, rt.Alchemy, rt.maxStackFor(ref.TypeFlags(), ref.Codename)) {
		builder.keys[key.name] = key
	}
	return builder
}

/*
================
forgeParseUint

A strict decimal value in [lo, hi]: no sign, no spaces (the command
text is already tokenized), nothing a hand could mistype past.
================
*/
func forgeParseUint(text string, lo, hi uint32) (uint32, bool) {
	if text == "" {
		return 0, false
	}
	value, err := strconv.ParseUint(text, 10, 32)
	if err != nil || value < uint64(lo) || value > uint64(hi) {
		return 0, false
	}
	return uint32(value), true
}

/*
================
forgeParseInt

forgeParseUint narrowed to the int the spec fields store. The key
table's ceiling already keeps every value far below this bound; the
explicit check states the bound at the conversion itself, so the
narrowing is provably lossless rather than trusted.
================
*/
func forgeParseInt(text string, lo, hi uint32) (int, bool) {
	parsed, ok := forgeParseUint(text, lo, hi)
	if !ok || parsed > math.MaxInt32 {
		return 0, false
	}
	return int(parsed), true
}

/*
================
forgeBuilder.apply

Judge one key=value token against the table and record it. Any
refusal - unknown key, a key named twice, a value outside its legal
set - refuses the whole command; nothing is built from a half spec.
================
*/
func (b *forgeBuilder) apply(token string) bool {
	eq := strings.IndexByte(token, '=')
	if eq <= 0 || eq == len(token)-1 {
		return false
	}
	name := strings.ToLower(token[:eq])
	value := token[eq+1:]
	key, ok := b.keys[name]
	if !ok || b.seen[name] {
		return false
	}
	b.seen[name] = true
	switch key.kind {
	case forgeKeyBound:
		// Forged items are always bound; only the acknowledgement is
		// a legal token, never the opt-out.
		return value == "1"
	case forgeKeyPlus:
		parsed, ok := forgeParseInt(value, key.lo, key.hi)
		if !ok {
			return false
		}
		b.spec.plus = parsed
		return true
	case forgeKeyWhiteAll:
		parsed, ok := forgeParseInt(value, key.lo, key.hi)
		if !ok {
			return false
		}
		b.spec.whiteAll = parsed
		return true
	case forgeKeyWhiteField:
		parsed, ok := forgeParseInt(value, key.lo, key.hi)
		if !ok {
			return false
		}
		b.spec.whiteFields[key.field] = parsed
		return true
	case forgeKeyBlue:
		if b.spec.blueFull || b.blueBy[key.option.ID] {
			return false
		}
		var parsed uint32
		if strings.EqualFold(value, "max") {
			parsed = key.rng.maxValue()
		} else {
			parsed64, ok := forgeParseUint(value, 0, math.MaxUint32)
			if !ok || !key.rng.accepts(parsed64) {
				return false
			}
			parsed = parsed64
		}
		b.blueBy[key.option.ID] = true
		b.spec.blues = append(b.spec.blues, forgeBlueGrant{option: key.option, value: parsed})
		return true
	case forgeKeyBlueFull:
		if value != "full" || len(b.spec.blues) > 0 {
			return false
		}
		b.spec.blueFull = true
		return true
	case forgeKeyDurability:
		parsed, ok := forgeParseUint(value, key.lo, key.hi)
		if !ok {
			return false
		}
		b.spec.durability = int64(parsed)
		return true
	case forgeKeyCount:
		parsed, ok := forgeParseInt(value, key.lo, key.hi)
		if !ok {
			return false
		}
		b.spec.count = parsed
		return true
	case forgeKeyTransform:
		refs, ok := b.rt.deps.ItemReferences().(enterworld.CharacterRefSource)
		if !ok {
			return false
		}
		monster, found := forgeCharacterRef(refs, value)
		if !found {
			return false
		}
		b.spec.transformRef = monster.RefObjID
		b.spec.transformSet = true
		return true
	case forgeKeyPetLevel:
		parsed, ok := forgeParseInt(value, key.lo, key.hi)
		if !ok {
			return false
		}
		b.spec.petLevel = parsed
		return true
	case forgeKeyPetName:
		if len(value) == 0 || len(value) > forgePetNameMaxBytes {
			return false
		}
		b.spec.petName = value
		b.spec.petNameSet = true
		return true
	}
	return false
}

/*
================
forgeEncodeOption

The wire u64 of one magic option: low param id, magnitude above
(alchemy/stones.go's encoding).
================
*/
func forgeEncodeOption(option alchemy.Magic, value uint32) uint64 {
	return uint64(value)<<32 | uint64(option.ID)
}

/*
================
forgeBlueCapacity

The most blues this forged set may carry: the capacity the catalogue
grants the set itself ('dura'/'nrep' raise it), exactly the alchemy
lanes' magicLimit over the candidate options.
================
*/
func forgeBlueCapacity(cat *alchemy.Catalog, ref *enterworld.ItemRef, grants []forgeBlueGrant) int {
	if cat == nil {
		return 0
	}
	probe := inventory.Item{Codename: ref.Codename}
	for _, grant := range grants {
		probe.MagicOptions = append(probe.MagicOptions, forgeEncodeOption(grant.option, grant.value))
	}
	return cat.MagicLimit(probe)
}

/*
================
forgeFullBlues

blue=full expanded: every offered option at its max value, in
param-id order, growing only while the set still fits its own
capacity (a 'dura'/'nrep' option raises the capacity as it lands).
================
*/
func (b *forgeBuilder) forgeFullBlues() []forgeBlueGrant {
	var grants []forgeBlueGrant
	for _, key := range b.blueKeysInOrder() {
		trial := append(append([]forgeBlueGrant{}, grants...), forgeBlueGrant{option: key.option, value: key.rng.maxValue()})
		if len(trial) > forgeBlueCapacity(b.cat, b.ref, trial) {
			continue
		}
		grants = trial
	}
	return grants
}

/*
================
forgeBuilder.blueKeysInOrder

The builder's blue.<OPTION> keys in param-id order (blue=full's
expansion order).
================
*/
func (b *forgeBuilder) blueKeysInOrder() []forgeKey {
	var keys []forgeKey
	for _, key := range b.keys {
		if key.kind == forgeKeyBlue {
			keys = append(keys, key)
		}
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i].option.ID < keys[j].option.ID })
	return keys
}

/*
================
forgeWhiteField

A white percent as a 5-bit variance field, rounded to the nearest
step: 0% is the floor field 0, 100% the ceiling field 31.
================
*/
func forgeWhiteField(percent int) uint64 {
	return uint64((percent*31 + 50) / 100)
}

/*
================
forgeBuilder.assemble

Build the forged item from the spec. Order matters: whites set the
variance word, blues encode, and only then is the durability ceiling
known - the current durability defaults to full at the final spec,
and an explicit durability key must fit under that same ceiling.
The item is born bound, whatever the spec said.
================
*/
func (b *forgeBuilder) assemble(now time.Time) (inventory.Item, bool) {
	ref := b.ref
	// The parser bounded every value already (forgeParseInt against the
	// key table); these refusals only re-assert the bounds where the
	// narrow conversions below can see them.
	if b.spec.count > math.MaxUint16 || b.spec.plus > math.MaxUint8 {
		return inventory.Item{}, false
	}
	item := inventory.Item{
		RefObjID: ref.RefObjID, Codename: ref.Codename,
		TypeFlags: ref.TypeFlags(), Quantity: 1, ForgeBound: true,
	}
	if b.spec.count >= 0 {
		item.Quantity = uint16(b.spec.count)
	}
	whiteFields := combat.WhiteStatFields(ref.TypeFlags())
	if len(whiteFields) > 0 {
		if b.spec.plus >= 0 {
			item.Plus = uint8(b.spec.plus)
		}
		bits := uint64(0)
		for _, field := range whiteFields {
			percent, named := b.spec.whiteFields[field.Field]
			if !named {
				percent = b.spec.whiteAll
			}
			if percent < 0 {
				continue
			}
			bits |= forgeWhiteField(percent) << (field.Field * 5)
		}
		item.VarianceBits = bits
	}
	grants := b.spec.blues
	if b.spec.blueFull {
		grants = b.forgeFullBlues()
	}
	if len(grants) > 0 {
		if b.cat == nil || len(grants) > forgeBlueCapacity(b.cat, b.ref, grants) {
			return inventory.Item{}, false
		}
		for _, grant := range grants {
			item.MagicOptions = append(item.MagicOptions, forgeEncodeOption(grant.option, grant.value))
		}
	}
	if len(whiteFields) > 0 && whiteFields[0].Name == "dur" {
		row := enterworld.InventoryRow{
			Codename:     ref.Codename,
			VarianceBits: strconv.FormatUint(item.VarianceBits, 10),
			MagicOptions: append([]uint64(nil), item.MagicOptions...),
		}
		maximum, err := combat.EquipmentMaxDurability(ref, row, b.rt.deps.MagicOptionDefinitions())
		if err != nil {
			return inventory.Item{}, false
		}
		item.Durability = maximum
		if b.spec.durability >= 0 {
			if b.spec.durability < 1 || uint64(b.spec.durability) > uint64(maximum) {
				return inventory.Item{}, false
			}
			item.Durability = uint32(b.spec.durability)
		}
	}
	if b.spec.transformSet {
		item.TransformRefObjID = b.spec.transformRef
	}
	if b.spec.petLevel >= 0 || b.spec.petNameSet {
		summon, ok := b.forgeSummon(now)
		if !ok {
			return inventory.Item{}, false
		}
		item.Summon = summon
	}
	return item, true
}

/*
================
forgeBuilder.forgeSummon

The pet record a pet.* key forges onto a COS summoner item: the
native level-row resolution (enterworld.SummonCharacterReference)
with the GM's level, full HP/MP and satiety, and - for a pickup pet
(character band 4) - the container and rental expiry the summon lane
builds at first use (cossummoning.go), so the forged record is one
the summon lane accepts as its own. The level is the record's stored
level: the row's own for threshold families, the GM's for the rest.
================
*/
func (b *forgeBuilder) forgeSummon(now time.Time) (*domain.CharacterCOS, bool) {
	refs, ok := b.rt.deps.ItemReferences().(enterworld.CharacterRefSource)
	if !ok {
		return nil, false
	}
	// The parser bounded petLevel to forgePetLevelMax already; this
	// refusal only re-asserts the bound where the u8 conversion below
	// can see it.
	if b.spec.petLevel > math.MaxUint8 {
		return nil, false
	}
	level := int64(1)
	if b.spec.petLevel >= 0 {
		level = int64(b.spec.petLevel)
	}
	charRef, found := enterworld.SummonCharacterReference(refs, b.ref, level)
	if !found || charRef == nil {
		return nil, false
	}
	summon := &domain.CharacterCOS{
		RefObjID: charRef.RefObjID, Codename: charRef.Codename,
		Level: charRef.Level, CurrentHP: charRef.MaxHP, CurrentMP: charRef.MaxMP,
		Satiety: initialCompanionSatiety, StateFlags: 1,
		CommandMode: initialAttackCompanionMode,
	}
	if len(b.ref.SummonLevelThresholds) == 0 && b.spec.petLevel >= 0 {
		summon.Level = uint8(b.spec.petLevel)
	}
	if charRef.TidWord>>11 == 4 {
		summon.CommandMode = initialPickupCompanionMode
		summon.Container = &domain.COSContainer{Capacity: b.rt.PetInventoryPages.GrabCapacity(initialPickupCompanionSlots)}
		minutes, present := b.ref.NativeFields.Lookup("itemParam1_29c")
		if !present || math.IsNaN(minutes) || math.IsInf(minutes, 0) ||
			minutes <= 0 || minutes > math.MaxInt32/60 || math.Trunc(minutes) != minutes {
			return nil, false
		}
		summon.RentalExpiresAtUnix = now.Unix() + int64(minutes)*60
	}
	if b.spec.petNameSet {
		summon.Name = b.spec.petName
	}
	return summon, true
}

/*
================
forgeCharacterRef

A character/monster reference by codename, tolerating the GM's case:
the exact row first, then the folded row (codenames are authored
upper-case ASCII).
================
*/
func forgeCharacterRef(source enterworld.CharacterRefSource, codename string) (*enterworld.CharacterRef, bool) {
	if ref, ok := source.CharacterRefByCodename(codename); ok && ref != nil && ref.RefObjID != 0 {
		return ref, true
	}
	if ref, ok := source.CharacterRefByCodename(strings.ToUpper(codename)); ok && ref != nil && ref.RefObjID != 0 {
		return ref, true
	}
	return nil, false
}

/*
================
forgeItemRef

An item reference by codename, with the same case tolerance as
forgeCharacterRef.
================
*/
func forgeItemRef(source interface {
	ItemRefByCodename(string) (*enterworld.ItemRef, bool)
}, codename string) (*enterworld.ItemRef, bool) {
	if ref, ok := source.ItemRefByCodename(codename); ok && ref != nil {
		return ref, true
	}
	if ref, ok := source.ItemRefByCodename(strings.ToUpper(codename)); ok && ref != nil {
		return ref, true
	}
	return nil, false
}
