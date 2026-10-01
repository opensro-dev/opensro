package wire

import "opensro.online/server/internal/domain"

// Result bursts: the ordered packet sequences the item plane answers with.
//
// The ORDER is part of the contract, not a presentation choice - the client
// applies the frames as they land, so a reordered burst produces a visibly
// different scene. Each composer mirrors the packet list of the corresponding
// server.mjs handler, which was probed live against the native client.
//
// THE PICKUP-DESPAWN TIMING CONTRACT (bug B, closed as verified-native):
// the 0x36AB despawn rides the SAME burst as the 0x35C7 scoop trigger and the
// 0xB06D grant. That is the native trigger - retail-family SR_GameServer
// sub_526090 sends the anim inline in its success tail and the despawn rides
// the item's world-removal in the same grant window. The 0.5-1s of visible
// linger after the scoop is the CLIENT's own dissolve: CIItem despawn
// (sub_86e980) arms a fixed 1.5s CIDecoDisappear fade (data_bd5144 = 1.5f,
// byte-verified). Deferring the despawn to an animation event would DOUBLE
// the linger and diverge from native - never do it.

// Frame is one native packet of a burst: the opcode and its encoded payload.
// It is the transport-agnostic unit the session layer wraps into its
// {nativeOpcode, payload} envelope.
type Frame struct {
	Scope   []domain.ObjectScopeChange `json:"-"`
	Opcode  uint16
	Payload []byte
	// Current is server-only delivery admission; never serialized on the wire.
	Current func() bool `json:"-"`
}

// ProgressionBroadcastFrames projects an acting character's ordered
// progression burst onto the public division channel. Retail's level
// transition owner broadcasts only the gid-bearing presentation packet;
// base stats, SP and EXP remain private character state. Keeping this
// projection beside the wire composers prevents every reward producer
// (combat, quests and the guarded diagnostic grant) from inventing its own
// routing policy.
func ProgressionBroadcastFrames(frames []Frame) []Frame {
	var public []Frame
	for _, frame := range frames {
		if frame.Opcode != OpLevelUpEffect && frame.Opcode != OpVisualFlagsUpdate {
			continue
		}
		payload := append([]byte(nil), frame.Payload...)
		public = append(public, Frame{Opcode: frame.Opcode, Payload: payload, Current: frame.Current, Scope: append([]domain.ObjectScopeChange(nil), frame.Scope...)})
	}
	return public
}

// ProgressionPrivateFrames is the actor-only complement of
// ProgressionBroadcastFrames. It deliberately removes the gid-bearing
// level-up presentation packet because an asynchronous combat owner first
// publishes that packet on the division route, then delivers this tail to
// the affected actor. Concatenating public then private therefore recreates
// the native semantic order without replaying 0x36B0 to the actor:
//
//	0x36B0 -> 0x343C -> optional 0x30B3 -> 0x30D2
//
// Plain EXP/SP changes have no public row, so their entire burst remains in
// this projection. Payloads are cloned because the two routes transfer
// ownership independently to the transport.
func ProgressionPrivateFrames(frames []Frame) []Frame {
	var private []Frame
	for _, frame := range frames {
		if frame.Opcode == OpLevelUpEffect || frame.Opcode == OpVisualFlagsUpdate {
			continue
		}
		payload := append([]byte(nil), frame.Payload...)
		private = append(private, Frame{Opcode: frame.Opcode, Payload: payload, Current: frame.Current, Scope: append([]domain.ObjectScopeChange(nil), frame.Scope...)})
	}
	return private
}

// PickupRefusalFrames is a failed pickup:
//
//	[0xB2CD release][0xB06D error]
//
// The latch release leads every refusal outcome because the client can never
// self-clear +0x618; the error itself surfaces through the 0xB06D notice
// (native notice category 0x01), not through 0xB2CD's kind-3 form.
func PickupRefusalFrames(errorCode uint8) []Frame {
	return []Frame{
		{Opcode: OpActionState, Payload: ReleaseActionState().Encode()},
		{Opcode: OpItemMoveResponse, Payload: EncodeItemMoveError(errorCode)},
	}
}

// PickupApproachArmFrame is the 0xB2CD arm an out-of-range pickup leads with.
// The movement ack toward the item follows it; that packet belongs to the
// movement plane and is appended by the caller.
func PickupApproachArmFrame() Frame {
	return Frame{Opcode: OpActionState, Payload: ArmActionState().Encode()}
}

// PickupGoldGrantFrames is a successful gold pickup:
//
//	[0xB2CD release][0x35C7 anim][0xB06D [1][6][0xFE][amount]]
//	[0x30B3 type 1 balance][0x36AB despawn]
//
// A gold heap is always consumed whole, so the despawn is unconditional. The
// 0xFE result already prints UIIT_MSG_STATE_GAIN_GOLD for the whole heap
// (client CPSMission_ApplyInventoryOperation), so a solo credit refreshes
// the balance silently (server 4EAD12 pushes notify 0). notify is set only
// when a party split credited this recipient (CParty_DistributeGold).
func PickupGoldGrantFrames(anim PickupAnim, amount uint32, balance uint64, itemGid uint32, notify bool) []Frame {
	return []Frame{
		{Opcode: OpActionState, Payload: ReleaseActionState().Encode()},
		{Opcode: OpPickupAnim, Payload: anim.Encode()},
		{Opcode: OpItemMoveResponse, Payload: EncodePickupGoldResult(amount)},
		{Opcode: OpPointsUpdate, Payload: GoldRefresh{Balance: balance, Notify: notify}.Encode()},
		{Opcode: OpObjectDespawn, Payload: ObjectDespawn{Gid: itemGid}.Encode(), Scope: []domain.ObjectScopeChange{{GID: itemGid}}},
	}
}

// PickupItemGrantFrames is a successful item pickup:
//
//	[0xB2CD release][0x35C7 anim][0xB06D [1][6][slot][CSOItem body]]
//	[0x36AB despawn - only when groundRemainder == 0]
//
// An over-cap pickup leaves the REMAINDER on the ground (the total > iMax arm
// of sub_756a60): the heap keeps its gid and its rendered entity, so the
// despawn is withheld - a client still drawing the drop is looking at a real,
// pickable object.
func PickupItemGrantFrames(anim PickupAnim, destSlot uint8, item ItemBody, itemGid uint32, groundRemainder uint16) []Frame {
	frames := []Frame{
		{Opcode: OpActionState, Payload: ReleaseActionState().Encode()},
		{Opcode: OpPickupAnim, Payload: anim.Encode()},
		{Opcode: OpItemMoveResponse, Payload: EncodePickupItemResult(destSlot, item)},
	}
	if groundRemainder == 0 {
		frames = append(frames, Frame{Opcode: OpObjectDespawn, Payload: ObjectDespawn{Gid: itemGid}.Encode(), Scope: []domain.ObjectScopeChange{{GID: itemGid}}})
	}
	return frames
}

// PickupBroadcastFrames is what the rest of the division sees of a grant:
// the scoop, and the despawn only when the drop was consumed whole.
func PickupBroadcastFrames(anim PickupAnim, itemGid uint32, groundRemainder uint16) []Frame {
	frames := []Frame{
		{Opcode: OpPickupAnim, Payload: anim.Encode()},
	}
	if groundRemainder == 0 {
		frames = append(frames, Frame{Opcode: OpObjectDespawn, Payload: ObjectDespawn{Gid: itemGid}.Encode(), Scope: []domain.ObjectScopeChange{{GID: itemGid}}})
	}
	return frames
}

// GoldDropFrames is a successful type-0x0A gold ground drop:
//
//	[0xB06D [1][0x0A][amount]][0x30B3 type 1 balance][0x30D7 spawn]
//
// The spawn row is forced onto the single-object form: a fresh drop always
// rides 0x30D7 with the appear tail that drives the drop-in presentation.
func GoldDropFrames(amount uint32, balance uint64, spawnRow GroundItemRow) []Frame {
	spawnRow.WithAppearTail = true
	spawnRow.AppearFlag = 1
	return []Frame{
		{Opcode: OpItemMoveResponse, Payload: EncodeGoldDropResult(amount)},
		{Opcode: OpPointsUpdate, Payload: GoldRefresh{Balance: balance}.Encode()},
		{Opcode: OpSingleObjectSpawn, Payload: spawnRow.Encode(), Scope: []domain.ObjectScopeChange{{GID: spawnRow.Gid, Visible: true}}},
	}
}

// GroundDropFrames is a successful type-0x07 item ground drop:
//
//	[0xB06D [1][7][src]][0x30D7 spawn]
func GroundDropFrames(sourceSlot uint8, spawnRow GroundItemRow) []Frame {
	spawnRow.WithAppearTail = true
	spawnRow.AppearFlag = 1
	return []Frame{
		{Opcode: OpItemMoveResponse, Payload: EncodeGroundDropResult(sourceSlot)},
		{Opcode: OpSingleObjectSpawn, Payload: spawnRow.Encode(), Scope: []domain.ObjectScopeChange{{GID: spawnRow.Gid, Visible: true}}},
	}
}

// DropBroadcastFrames is what the rest of the division sees of a ground or
// gold drop: just the spawn.
func DropBroadcastFrames(spawnRow GroundItemRow) []Frame {
	spawnRow.WithAppearTail = true
	spawnRow.AppearFlag = 1
	return []Frame{
		{Opcode: OpSingleObjectSpawn, Payload: spawnRow.Encode(), Scope: []domain.ObjectScopeChange{{GID: spawnRow.Gid, Visible: true}}},
	}
}
