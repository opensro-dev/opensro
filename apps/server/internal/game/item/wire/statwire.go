package wire

// The stat-allocation and mastery-training wire surface (v1.150).
//
// Provenance: the C->S senders and S->C acks were recovered from the client
// pseudocode dump by two independent passes. The classic 0x7050/0x7051 stat
// opcodes documented for other Silkroad builds are NOT this client's - the
// dump carries no such immediates, and the DevKit tree that hardcodes them is
// a different version.
//
//	+STR  CIFPlayerInfo GDR_PI_BTN_ADDHP (id 14) -> sub_59fa80 @0x0059fa80
//	      -> sub_692a90(0x4b, 1) @0x00692b47 -> 0x727A, EMPTY body
//	+INT  CIFPlayerInfo GDR_PI_BTN_ADDMP (id 15) -> sub_59faa0 @0x0059faa0
//	      -> sub_692a90(0x4b, 2) @0x00692af2 -> 0x7552, EMPTY body
//	train CIFSkillPracticeBox confirm sub_5de690 @0x005de771
//	      -> sub_6fee40 @0x006fee40 -> 0x7165 [u32 masteryId][u8 amount]
const (
	// OpAllocStrRequest is the client's "+1 STR" click. EMPTY payload:
	// sub_692a90 constructs the packet and submits it with no
	// AppendBytes call between (@0x00692b47 ctor, @0x00692b70 submit).
	OpAllocStrRequest uint16 = 0x727A
	// OpAllocIntRequest is the client's "+1 INT" click. EMPTY payload.
	OpAllocIntRequest uint16 = 0x7552
	// OpMasteryLevelUpRequest is the mastery-training confirm.
	// Payload [u32 masteryId][u8 amount]; the UI only ever sends amount 1
	// (sub_6fee40 @0x006feead/@0x006feebd).
	OpMasteryLevelUpRequest uint16 = 0x7165
	// OpSkillLearnRequest is the skill-learn confirm (the skill board's
	// confirm callback sub_5882c0 stashes the skill id @0x0058834b and
	// calls the sender sub_6fed80 @0x006fed80, which constructs 0x72CB
	// @0x006fedc1 and appends exactly the u32 skill id @0x006feded).
	// Payload [u32 skillId].
	OpSkillLearnRequest uint16 = 0x72CB

	// OpAllocStrResponse acks 0x727A. Handler sub_75ba00 @0x0075ba00:
	// result 1 decrements the client's remaining stat points (the u16 at
	// CICPlayer+0x83c) and refreshes the character window.
	OpAllocStrResponse uint16 = 0xB27A
	// OpAllocIntResponse acks 0x7552. Handler sub_75ba50 @0x0075ba50.
	OpAllocIntResponse uint16 = 0xB552
	// OpMasteryLevelUpResponse acks 0x7165. Handler sub_75bc00
	// @0x0075bc00: result 1 writes the new level into the mastery record
	// (record+4 via sub_8505a0) and refreshes the skill window.
	OpMasteryLevelUpResponse uint16 = 0xB165
	// OpSkillLearnResponse acks 0x72CB. Handler sub_75bb20 @0x0075bb20:
	// result 1 reads the u32 skill id, ASSERTS the skill's group is not
	// already learned at that level or higher (@0x0075bb71 - a wrong
	// success ack trips a debug breakpoint), marks it learned
	// (sub_8509f0, which REPLACES the group's previous-level entry) and
	// refreshes; result 2 reads one error byte routed to guide category
	// 5 (@0x0075bbe9).
	OpSkillLearnResponse uint16 = 0xB2CB

	// OpPointsUpdate carries an ABSOLUTE points value on a typed channel.
	// Handler sub_779c70; type 2 is skill points, type 3 stat points.
	OpPointsUpdate uint16 = 0x30B3
	// OpBaseStats is the login/base-stat staging block. Handler
	// sub_75be90 @0x0075be90: the ONLY channel that moves the live STR/INT
	// words (CICPlayer+0x834/+0x836) - char-data never carries them.
	OpBaseStats uint16 = 0x343C

	// OpExpUpdate is the experience / skill-exp delta channel (levelup
	// wave, LANE-1). Handler sub_779620 @0x00779620 (registrar opcode
	// write @0x0074d8d4, handler ptr @0x0074d8dc): adds the SIGNED exp
	// delta to the player's u64 exp (CICPlayer+0x828/+0x82c), walks the
	// leveldata curve itself (sub_7e0f20(level) row +0x08 u64 = column 1
	// of leveldata.txt) and increments the level byte +0x820 per
	// crossing - THE LEVEL-UP IS DERIVED CLIENT-SIDE FROM THIS PACKET,
	// there is no separate "set level" opcode in the registrar. When one
	// or more levels are crossed the handler reads a TRAILING u16
	// straight into the stat-points word +0x83c (@0x7797b0) - see
	// EncodeExpUpdate for the hazard that creates. The skill-exp delta
	// accumulates at +0x830 and wraps mod 0x190=400 (@0x779b02..
	// 0x779b29); the SP counter +0x838 is NOT touched here - SP still
	// rides 0x30B3 type 2 only.
	OpExpUpdate uint16 = 0x30D2
	// OpLevelUpEffect is the level-up presentation notification. Handler
	// sub_777670 @0x00777670 (registrar @0x0074d8b3/@0x0074d8bb):
	// payload is exactly [u32 gid]; the handler resolves the entity
	// (sub_852730, miss = silent return) and plays the SYSTEM_LEVELUP
	// effect 0x80000006 (COS/pet entities take SYSTEM_PET_LEVEL_UP
	// 0x80000022 instead, sub_a05510 vs data_cedc38) plus the
	// "snd_levup" sound (@0x777707). Pure presentation: no player state
	// moves. The 0x30D2 handler itself never references the effect
	// (verified: sub_779620's body carries no 0x80000006 use), so a
	// level-up burst that wants the retail visual must send BOTH.
	OpLevelUpEffect uint16 = 0x36B0
)

// Points-update channel types (sub_779c70's leading discriminator byte).
const (
	// PointsTypeGold is [u8 1][u64 balance][u8 notify].
	PointsTypeGold uint8 = 1
	// PointsTypeSkill is [u8 2][u32 skillPoints][u8 notify]: an ABSOLUTE
	// skill-point total, not a delta. The handler diffs it against the
	// stored value to pick its gain/loss notice.
	PointsTypeSkill uint8 = 2
	// PointsTypeStat is [u8 3][u16 statPoints]: an ABSOLUTE remaining-stat
	// count, stored as a WORD.
	//
	// DO NOT send this on a successful stat allocation: the 0xB27A/0xB552
	// ack already decrements the client's counter, so an absolute update
	// riding the same burst would double-apply the spend.
	PointsTypeStat uint8 = 3
	// PointsTypeHwan is [u8 4][...]: the hwan/berserk channel.
	PointsTypeHwan uint8 = 4
)

// MasteryLevelUpRequest is a decoded 0x7165 body.
type MasteryLevelUpRequest struct {
	MasteryID uint32
	// Amount is the level count the client asks to train. The native UI
	// hardcodes 1 (sub_6fee40's caller stacks the literal); a bulk value
	// is unmodelled, so the server refuses anything else rather than
	// guessing how retail batches the SP spend.
	Amount uint8
}

// DecodeMasteryLevelUpRequest reads the 0x7165 body.
func DecodeMasteryLevelUpRequest(payload []byte) (MasteryLevelUpRequest, error) {
	r := NewReader(payload)
	masteryID, err := r.U32()
	if err != nil {
		return MasteryLevelUpRequest{}, err
	}
	amount, err := r.U8()
	if err != nil {
		return MasteryLevelUpRequest{}, err
	}
	if err := r.Done(); err != nil {
		return MasteryLevelUpRequest{}, err
	}
	return MasteryLevelUpRequest{MasteryID: masteryID, Amount: amount}, nil
}

// EncodePointsAck is the shared success/error shape of the stat acks
// (0xB27A/0xB552): [u8 1] on success, [u8 2][u8 errorCode] on refusal.
// sub_75ba00 reads the result byte, and only on result 2 reads the error.
func EncodePointsAck(success bool, errorCode uint8) []byte {
	if success {
		return NewWriter(1).U8(ResultSuccess).Payload()
	}
	return NewWriter(2).U8(ResultError).U8(errorCode).Payload()
}

// EncodeMasteryLevelUpAck is a successful 0xB165:
// [u8 1][u32 masteryId][u8 newLevel]. The client writes newLevel straight
// into the mastery record (sub_75bc00 @0x0075bca0), so this value IS what
// the skill window shows - it must be the post-training level.
func EncodeMasteryLevelUpAck(masteryID uint32, newLevel uint8) []byte {
	return NewWriter(6).U8(ResultSuccess).U32(masteryID).U8(newLevel).Payload()
}

// EncodeMasteryLevelUpError is a refused 0xB165: [u8 2][u8 errorCode]. The
// client routes the code through the mastery guide group (notice category
// 0x07), which is why the codes below are that group's members.
func EncodeMasteryLevelUpError(errorCode uint8) []byte {
	return NewWriter(2).U8(ResultError).U8(errorCode).Payload()
}

// Mastery-training refusal codes. The client maps these through notice
// category 0x07 (the mastery guide group) in sub_689420; the keys named
// below are the ones that group already carries, so a refusal surfaces the
// retail message without any client work.
const (
	// ErrCodeMasterySkillPoints is UIIT_STT_SKILL_POINT_INSUFFICIENCY_MASTERY
	// (07:02): the trained level costs more SP than the character holds.
	ErrCodeMasterySkillPoints uint8 = 0x02
	// ErrCodeMasteryLevelLimit is the per-mastery ceiling (07:04): the next
	// level would pass the character level or the absolute 120 cap.
	ErrCodeMasteryLevelLimit uint8 = 0x04
	// ErrCodeMasteryTotalLimit is UIIT_STT_SKILL_LEARN_MASTERY_TOTAL_LIMIT
	// (07:05): the sum of trained mastery levels would pass the character's
	// allowance. Enforced by progression.HandleMasteryLevelUp against the
	// budget the v1.150 client itself displays (sub_58c310 @0x0058c310:
	// CH flat 300, EU min(2 x level, 240), selected on the native country
	// byte), pinned from the client's display sites.
	ErrCodeMasteryTotalLimit uint8 = 0x05
)

// SkillLearnRequest is a decoded 0x72CB body.
type SkillLearnRequest struct {
	SkillID uint32
}

// DecodeSkillLearnRequest reads the 0x72CB body: exactly one u32 (the
// sender sub_6fed80 appends 4 bytes and submits, @0x006feded).
func DecodeSkillLearnRequest(payload []byte) (SkillLearnRequest, error) {
	r := NewReader(payload)
	skillID, err := r.U32()
	if err != nil {
		return SkillLearnRequest{}, err
	}
	if err := r.Done(); err != nil {
		return SkillLearnRequest{}, err
	}
	return SkillLearnRequest{SkillID: skillID}, nil
}

// EncodeSkillLearnAck is a successful 0xB2CB: [u8 1][u32 skillId]. The
// client marks exactly this id learned (sub_75bb20 @0x0075bb48 reads the
// u32, sub_8509f0 inserts it), so it must echo the request's id.
func EncodeSkillLearnAck(skillID uint32) []byte {
	return NewWriter(5).U8(ResultSuccess).U32(skillID).Payload()
}

// EncodeSkillLearnError is a refused 0xB2CB: [u8 2][u8 errorCode]. The
// client routes the code through guide category 5 (sub_75bb20
// @0x0075bbe9 -> sub_689420), whose recovered table is the codes below.
func EncodeSkillLearnError(errorCode uint8) []byte {
	return NewWriter(2).U8(ResultError).U8(errorCode).Payload()
}

// Skill-learn refusal codes. Guide category 5's dispatcher maps EXACTLY
// three codes to notice keys (sub_689420 @0x0068a219/@0x0068a21e/
// @0x0068a223); every other byte falls through with no retail notice.
const (
	// ErrCodeSkillLearnStr is UIIT_STT_SKILL_LEARN_STR_INSUFFICIENCY
	// (05:03): character STR below the skilldata ReqStr column. Shipped
	// skilldata carries 0 in every row, so this only fires on custom
	// data (the itemdata ReqStr/ReqInt posture).
	ErrCodeSkillLearnStr uint8 = 0x03
	// ErrCodeSkillLearnInt is UIIT_STT_SKILL_LEARN_INT_INSUFFICIENCY
	// (05:04): the INT twin.
	ErrCodeSkillLearnInt uint8 = 0x04
	// ErrCodeSkillLearnSP is UIIT_STT_SKILL_POINT_INSUFFICIENCY (05:0a):
	// the learn costs more SP (skilldata m_nReq_Sp) than the character
	// holds.
	ErrCodeSkillLearnSP uint8 = 0x0a
)

// ErrCodeSkillLearnRefused is the error byte for every refusal the retail
// UI prevents composing (unknown skill id, mastery requirement unmet,
// prerequisite group unmet, group already learned at the level, malformed
// body). Category 5 maps no notice for it - deliberately: the pinned
// codes above would show a WRONG retail message for these causes, and no
// capture pins what retail sends for states its own UI forbids. The value
// is documented as UNVERIFIED (the ErrCodeStatAllocRefused precedent).
const ErrCodeSkillLearnRefused uint8 = 0x01

// ErrCodeStatAllocRefused is the error byte the stat acks carry on refusal.
//
// Pinned (LANE-3): UNLIKE the mastery/skill acks, the v1.150 stat handlers
// sub_75ba00 / sub_75ba50 READ this byte and then DISCARD it - they never
// call the guide dispatcher sub_689420 (contrast sub_75bc00 @0x0075bc00,
// which routes the mastery refusal to cat 7). So the byte produces NO client
// notice regardless of value, and retail never triggers the refusal anyway
// (the plus buttons only enable while CICPlayer+0x83c > 0, @0x005a01f8). 0x02
// is an arbitrary-but-harmless placeholder; the value is behaviourally inert.
const ErrCodeStatAllocRefused uint8 = 0x02

// EncodePointsStatUpdate is 0x30B3 type 3: [u8 3][u16 statPoints], the
// ABSOLUTE remaining stat points. For a pool that moved without its own ack
// (a stat recall); never alongside an allocation ack.
func EncodePointsStatUpdate(statPoints uint16) []byte {
	return NewWriter(3).U8(PointsTypeStat).U16(statPoints).Payload()
}

// EncodePointsSkillUpdate is 0x30B3 type 2: [u8 2][u32 skillPoints][u8
// notify]. The value is the character's ABSOLUTE remaining skill points.
//
// notify 0 stores the value silently; 1 lets the client raise its
// gain/loss guide notice from the diff against the value it held. A
// training spend uses 0: the mastery ack is the player-visible feedback,
// and a "you lost SP" toast would be wrong for a purchase.
func EncodePointsSkillUpdate(skillPoints uint32, notify bool) []byte {
	flag := uint8(0)
	if notify {
		flag = 1
	}
	return NewWriter(6).U8(PointsTypeSkill).U32(skillPoints).U8(flag).Payload()
}

// EncodeExpUpdate is the 0x30D2 body:
//
//	[u32 sourceGid][s32 expDelta][s32 skillExpDelta][u8 flags]
//	+ [u16 statPoints]  ONLY when the deltas cross >= 1 level upward
//
// Field semantics, pinned against sub_779620:
//
//   - sourceGid is only ever nonzero-CHECKED (@0x779b69) to gate the
//     exp-gauge animation; retail rides the granting entity's gid, 0
//     plays no animation. The value itself is never dereferenced.
//   - expDelta is SIGNED (the handler sign-extends it into the 64-bit
//     add, @0x779779). It must be the delta the server ACTUALLY APPLIED:
//     the client re-walks the same leveldata curve from this delta, so
//     an emitted value larger than the applied one (e.g. a grant the
//     server clamped at the level cap) would walk the client past the
//     server's state.
//   - skillExpDelta accumulates into the client's mod-400 gauge word.
//   - flags is ALWAYS 0 from this server: bit patterns (f&0x0f)==1 and
//     (f&0xf0)==0x10 make the handler read extra dwords whose meaning is
//     unpinned (base-stat singleton +0x2f0 via sub_818930; the
//     sub_827c30/sub_827c80 paths) - never emit what cannot be named.
//   - statPoints is the ABSOLUTE post-grant stat-point pool, read
//     straight into CICPlayer+0x83c (@0x7797b0) - the same word the
//     0xB27A/0xB552 acks decrement and 0x30B3 type 3 overwrites. The
//     tail must be present EXACTLY when the client's own curve walk
//     crosses a level (the server walks the same shipped curve, so
//     "levelled server-side" is that condition), and a levelling burst
//     must NEVER also carry a type-3 update (the documented
//     double-apply trap family).
func EncodeExpUpdate(sourceGid uint32, expDelta, skillExpDelta int32, levelled bool, statPoints uint16) []byte {
	w := NewWriter(15).
		U32(sourceGid).
		U32(uint32(expDelta)).
		U32(uint32(skillExpDelta)).
		U8(0) // flags: both conditional branches skipped
	if levelled {
		w.U16(statPoints)
	}
	return w.Payload()
}

// EncodeLevelUpEffect is the 0x36B0 body: exactly [u32 gid], the entity
// the SYSTEM_LEVELUP effect and snd_levup sound play on (sub_777670
// reads 4 bytes @0x77767d and resolves by gid; an unknown gid is a
// silent no-op client-side).
func EncodeLevelUpEffect(gid uint32) []byte {
	return NewWriter(4).U32(gid).Payload()
}

// BaseStats is the 0x343C staging block (sub_75be90's read order).
//
// The handler stages every field into the singleton at 0xced160. The local
// player-info pane reads the first eight fields from that singleton, while
// four later fields are also applied directly to CICPlayer:
//
//	MaxHP    -> vtable +0x98 (sub_862990) -> CICPlayer+0x448
//	MaxMP    -> vtable +0x9c (sub_8629c0) -> CICPlayer+0x44c
//	StrWord  -> sub_8629f0 -> CICPlayer+0x834  (the equip/tooltip STR)
//	IntWord  -> sub_862a00 -> CICPlayer+0x836  (the equip/tooltip INT)
//
// TRAP: the +0x14/+0x16 words are NOT the applied STR/INT despite reading
// like it. cmd/active-probe's former builder wrote the stats there and
// left +0x20/+0x22 zero - a player built from that block gets STR/INT 0 -
// until the progression wave pointed it at this encoder. Any production
// emission must use StrWord/IntWord below.
type BaseStats struct {
	// Combat display fields consumed by CIFPlayerInfo::UpdateStats
	// (sub_59ffa0). They are not copied onto CICPlayer, but they are live
	// UI state: zeroing them produces the all-zero attack/defence pane.
	PhysicalAttackMin uint32
	PhysicalAttackMax uint32
	MagicalAttackMin  uint32
	MagicalAttackMax  uint32
	PhysicalDefense   uint16
	MagicalDefense    uint16
	HitRate           uint16
	ParryRate         uint16

	// MaxHP/MaxMP are applied to the player, so they must carry the
	// character's real maxima - a zero here empties the HUD gauges.
	MaxHP uint32
	MaxMP uint32

	// StrWord/IntWord are block offsets +0x20/+0x22: the ONLY inputs to
	// the live STR/INT words the equip gates and the item-tooltip
	// requirement rows compare against.
	StrWord uint16
	IntWord uint16
}

// BaseStatsSize is the block's byte length (0x24): four u32 + four u16 +
// two u32 + two u16.
const BaseStatsSize = 0x24

// Encode serializes the block in sub_75be90's read order.
func (b BaseStats) Encode() []byte {
	return NewWriter(BaseStatsSize).
		U32(b.PhysicalAttackMin).
		U32(b.PhysicalAttackMax).
		U32(b.MagicalAttackMin).
		U32(b.MagicalAttackMax).
		U16(b.PhysicalDefense).
		U16(b.MagicalDefense).
		U16(b.HitRate).
		U16(b.ParryRate).
		U32(b.MaxHP).
		U32(b.MaxMP).
		U16(b.StrWord).
		U16(b.IntWord).
		Payload()
}
