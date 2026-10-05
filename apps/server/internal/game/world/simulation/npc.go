package simulation

import (
	"fmt"
	"math"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/item/wire"
)

// NpcDef is one roster NPC. TidWord is the RE-pinned RefObj typeflag word the
// client's sub_851420 cascade classifies as NPC (0x0146 for the probe row).
type TeleportGateBounds struct {
	FortressID uint32  `json:"fortressId,omitempty"`
	Radius     float64 `json:"radius"`
	Height     float64 `json:"height"`
}

type NpcDef struct {
	// Static gate rows share world interest/liveness, but never the NPC wire schema.
	Teleport *TeleportGateBounds

	// ObjectID is the stable division-world identity for this NPC position.
	// Every roster row must provide one; NPC identity never depends on the
	// observing character or on roster position at runtime.
	ObjectID  uint32
	RefObjID  uint32
	TidWord   uint16
	Codename  string
	NameStrID string
	Name      string
	ModelPath string
	Level     uint8
	MaxHP     uint32
	// BaseSpeechSymbol / QuestSpeechSymbol are npcchat.txt's BS/PS columns.
	// The generic 0x7338 talk action returns the BS symbol through the native
	// 0x3773 dialog wire; neither the server nor the browser invents prose.
	BaseSpeechSymbol  string
	QuestSpeechSymbol string
	// TalkFlags is the fully composed 0xB45A capability dword for this exact
	// roster row. It is resolved while the shipped NPC, npcchat, refshop and
	// server-service data are joined; packet handlers must not re-derive it
	// from a second codename allowlist.
	TalkFlags uint32
	// Services is the NPC's service option set (npcservice.go), which
	// handlers check (CGObj_HasService 484DF0) and TalkFlags projects.
	Services NpcServices
	// AuthoredSpawn distinguishes npcpos.txt world coordinates from the
	// synthetic anchor used by the old fixture.
	AuthoredSpawn bool
	Spawn         Spawn
	WalkSpeed     float64
	RunSpeed      float64
	ScaleDenom    float64
	// SpawnOffsetX/Z separate authored roster entries around the shared
	// anchor. Index zero remains byte-identical to the original smith row.
	SpawnOffsetX float64
	SpawnOffsetZ float64
	// Patrol enables the fixture's triangle-wave movement. Fixed machinery
	// such as NPC_CH_GACHA_MACHINE must never walk away from its station.
	Patrol bool
	// RebirthPoint is the town-safe position this teleport guide appoints.
	// A zero RegionID means the NPC has no appointment service.
	RebirthPoint Spawn
	// NpcTalkStoreGroups is the v1.150 refshopgroup -> refmappingshopgroup
	// -> refmappingshopwithtab -> refshoptab media chain projected into the
	// exact rows consumed by client sub_5d5be0. It belongs beside the roster
	// row because both are RefObj/PK2 bootstrap data, not packet policy.
	NpcTalkStoreGroups []NpcTalkStoreGroup
}

// ValidateNpcRoster enforces the static-world identity contract at composition
// time. The NPC band is exclusive at both ends: its base is not an entity and
// the ground-item base belongs to a different authority.
func ValidateNpcRoster(roster []NpcDef) error {
	seen := make(map[uint32]struct{}, len(roster))
	for index, npc := range roster {
		if npc.ObjectID <= domain.NPCGIDBase || npc.ObjectID >= domain.GroundItemGIDBase {
			return fmt.Errorf(
				"NPC roster row %d (%s) has object id %d outside (%d,%d)",
				index, npc.Codename, npc.ObjectID, domain.NPCGIDBase, domain.GroundItemGIDBase,
			)
		}
		if _, duplicate := seen[npc.ObjectID]; duplicate {
			return fmt.Errorf("NPC roster row %d (%s) duplicates object id %d", index, npc.Codename, npc.ObjectID)
		}
		seen[npc.ObjectID] = struct{}{}
	}
	return nil
}

type NpcTalkStoreTab struct {
	TabID            int32
	LabelSymbol      string
	GroupID          int32  `json:",omitempty"`
	GroupLabelSymbol string `json:",omitempty"`
}

type NpcTalkStoreGroup struct {
	// StoreGroupID is the NPC's reference ID, a DWORD.
	StoreGroupID uint32
	Tabs         []NpcTalkStoreTab
}

// defaultNpcRoster is the single source of truth for NPCs spawned into the
// object list. Callers receive copies so no lane can mutate global world
// policy.
var defaultNpcRoster = [...]NpcDef{
	{
		ObjectID:  domain.NPCGIDBase + 1,
		RefObjID:  7495,
		TidWord:   0x0146,
		Codename:  "NPC_EU_SMITH",
		NameStrID: "SN_NPC_EU_SMITH",
		Name:      "Weapon Trader Balbardo",
		Patrol:    true,
		// The fixture smith talks; DefaultNpcRoster adds 4C6350's services.
		Services: NpcServices(0).With(NpcServiceShop, NpcServiceTalk),
		// Media v1.150:
		// refshopgroup GROUP_STORE_EU_SMITH -> NPC_EU_SMITH;
		// refshoptab STORE_EU_SMITH_TAB1..3 ids 2015..2017 and the three
		// authored label symbols. The native row payload stores the selected
		// character RefObj id (7495) in record+0x0c.
		NpcTalkStoreGroups: []NpcTalkStoreGroup{{
			StoreGroupID: 7495,
			Tabs: []NpcTalkStoreTab{
				{TabID: 2015, LabelSymbol: "SN_TAB_WEAPON"},
				{TabID: 2016, LabelSymbol: "SN_TAB_SHIELD"},
				{TabID: 2017, LabelSymbol: "UIIT_CTL_WNETWORK_BOLT"},
			},
		}},
	},
	{
		ObjectID:     domain.NPCGIDBase + 2,
		RefObjID:     9251,
		TidWord:      0x0146,
		Codename:     "NPC_CH_GACHA_MACHINE",
		NameStrID:    "SN_NPC_CH_GACHA_MACHINE",
		Name:         "Magic Pop",
		SpawnOffsetX: 24,
		Patrol:       false,
	},
	{
		ObjectID:     domain.NPCGIDBase + 3,
		RefObjID:     19519,
		TidWord:      0x0146,
		Codename:     "NPC_EU_ADVICE3",
		NameStrID:    "SN_NPC_EU_ADVICE3",
		Name:         "Guide Riise",
		Services:     NpcServices(0).With(NpcServiceTalk),
		SpawnOffsetX: 48,
		Patrol:       false,
	},
}

// DefaultNpcRoster returns an independently owned roster.
func DefaultNpcRoster() []NpcDef {
	roster := append([]NpcDef(nil), defaultNpcRoster[:]...)
	for index := range roster {
		groups := append(
			[]NpcTalkStoreGroup(nil),
			roster[index].NpcTalkStoreGroups...,
		)
		for groupIndex := range groups {
			groups[groupIndex].Tabs = append(
				[]NpcTalkStoreTab(nil),
				groups[groupIndex].Tabs...,
			)
		}
		roster[index].NpcTalkStoreGroups = groups
		roster[index].Services |= ResolveNpcServices(roster[index])
		roster[index].TalkFlags = ResolveNpcTalkFlags(roster[index])
	}
	return roster
}

var npcShopSpawn = Spawn{RegionID: 25511, X: 941.5, Y: 7.2, Z: 1417.2}

// NpcShopSpawn returns the fixed Constantinople shop anchor used when NPCs
// are not spawned at the player.
func NpcShopSpawn() Spawn {
	return npcShopSpawn
}

// Patrol tuning (server.mjs missionNpcPatrolSpanZ / missionNpcPatrolStepPerTick).
// The pattern is fixture policy - each NPC ping-pongs between its spawn point
// and +20u on Z at 5u per coarse tick - but the emitted 0x30E3 bytes are
// asm-pinned (sub_775cb0) and must match the reference builder exactly.
const (
	NpcPatrolSpanZ       = 20.0
	NpcPatrolStepPerTick = 5.0
)

// PatrolState is one NPC's authoritative pose for a tick index.
type PatrolState struct {
	RegionID    uint16
	X, Y, Z     float64
	HeadingWord uint16
}

// ComputeNpcPatrolState is a pure function of the tick index (no hidden timer
// state), server.mjs computeNpcPatrolState: a triangle wave over
// [0, 2*legTicks) - 0..legTicks is A->B, then B->A - with the heading facing
// the direction of travel.
//
// THE HEADING WAS MIRRORED UNTIL NOW, AND THE PARITY FIXTURE PINNED THE MIRROR.
// This function used to document and emit "+z -> 0, -z -> pi". Native is the
// opposite: yaw 0 faces -Z (Math_YawToDirVec sub_8788c0 = {sin(yaw), 0,
// -cos(yaw)}, cos negation at 0x8788f0; Math_DirVecToYaw sub_8791a0 inverts as
// acos(-z/len) mirrored for x<0, negation at 0x8791f7 - REV's binary pin, board
// seq742). This patrol is pure +/-Z travel, and +/-Z is exactly where the
// mirror is a FULL pi rather than a partial error, so NPC_EU_SMITH faced
// exactly backwards on both legs, always - the same defect class as BUG-11 on
// the monster plane (coordinator seq734).
//
// PROVENANCE OF THE FIXTURE CHANGE (board seq752, G-SRV co-signed seq763; the
// fixture rows for patrol and npcWire move/correction payloads were
// regenerated). The old Node reference CONTRADICTED ITSELF, so this is not
// "native beats our reference", it is "the reference disagreed with the
// reference": testdata/gen_parity_vectors.mjs:232 computes
// missionHeadingFromMovement as atan2(dx, -dz) - already native-correct - while
// its computeNpcPatrolState did not call that helper and hardcoded
// `yaw = movingToB ? 0 : Math.PI` instead. Feeding this patrol's real +z leg
// (dx=0, dz=+20) through the reference's OWN helper yields pi, the opposite of
// its own literal. The fixture was therefore pinning a bypassed-helper literal,
// not a wire contract, and server.mjs was our own prototype rather than a
// captured retail stream - so it never outranked the client binary.
//
// The heading is now DERIVED from the leg's actual delta through the one shared
// encoder (headingWordFromDelta, geometry.go) instead of being written as a
// literal. Re-hardcoding the corrected constant would have repeated the exact
// root cause with a better value; deriving it makes this agree with the player
// plane's HeadingFromMovement by construction rather than by coincidence.
func ComputeNpcPatrolState(anchor Spawn, tick int64) PatrolState {
	ax := clampFloat(anchor.X+8, 0, 0xffff)
	ay := anchor.Y
	az := clampFloat(anchor.Z+5, 0, 0xffff)
	bz := clampFloat(az+NpcPatrolSpanZ, 0, 0xffff)

	legTicks := int64(math.Max(1, math.Ceil(math.Abs(bz-az)/NpcPatrolStepPerTick)))
	period := 2 * legTicks
	phase := ((tick % period) + period) % period
	var t float64
	if phase <= legTicks {
		t = float64(phase) / float64(legTicks)
	} else {
		t = float64(period-phase) / float64(legTicks)
	}
	z := az + (bz-az)*t

	movingToB := phase <= legTicks
	// The A->B leg travels +z and B->A travels -z; the shared encoder owns
	// what those directions MEAN on the wire.
	legDz := bz - az
	if !movingToB {
		legDz = -legDz
	}
	headingWord := headingWordFromDelta(0, legDz)

	return PatrolState{
		RegionID:    anchor.RegionID,
		X:           ax,
		Y:           ay,
		Z:           z,
		HeadingWord: headingWord,
	}
}

/*
================
NpcStation

Where a roster NPC is published: its npcpos.txt position, or for the
synthetic fixture roster a few units from the anchor (the same +8/+5 base
the patrol A-point uses) plus the row's reviewed station offset. Every
consumer that places or measures an NPC goes through here.
================
*/
func NpcStation(npc NpcDef, anchor Spawn) Spawn {
	if npc.AuthoredSpawn {
		return npc.Spawn
	}
	return Spawn{
		RegionID: anchor.RegionID,
		X:        clampFloat(anchor.X+8+npc.SpawnOffsetX, 0, 0xffff),
		Y:        anchor.Y,
		Z:        clampFloat(anchor.Z+5+npc.SpawnOffsetZ, 0, 0xffff),
		Angle:    anchor.Angle,
	}
}

// BuildNpcCreateRow encodes one object-list create row (sub_777220 ->
// CICNPC sub_8625f0), byte-for-byte the reference buildV150NpcCreateRow:
// RefObjID, the shared sub_85fb20 object block (object id, region, xyz,
// heading, packed movement, speeds, ref count), then the sub_859d40 name
// mask + name. Rides the 0x3417 object-list chunk (bootstrap) - the
// consumer wraps it.
func BuildNpcCreateRow(npc NpcDef, anchor Spawn) []byte {
	station := NpcStation(npc, anchor)
	anchor.RegionID = station.RegionID
	x, y, z, heading := station.X, station.Y, station.Z, station.Angle
	walkSpeed := npc.WalkSpeed
	runSpeed := npc.RunSpeed
	scaleDenom := npc.ScaleDenom
	if !npc.AuthoredSpawn {
		walkSpeed = WalkSpeed
		runSpeed = RunSpeed
	}
	if walkSpeed < 0 {
		walkSpeed = 0
	}
	if runSpeed < 0 {
		runSpeed = 0
	}
	if scaleDenom <= 0 {
		scaleDenom = 100
	}

	if npc.Teleport != nil {
		return wire.NewWriter(24).U32(npc.RefObjID).U32(npc.ObjectID).U16(anchor.RegionID).F32(float32(x)).F32(float32(y)).F32(float32(z)).U16(heading).Payload()
	}
	w := wire.NewWriter(64)
	w.U32(npc.RefObjID).
		U32(npc.ObjectID).
		U16(anchor.RegionID).
		F32(float32(x)).
		F32(float32(y)).
		F32(float32(z)).
		U16(heading).
		U8(0).
		U8(1).
		U8(0).
		U16(0).
		U8(0).
		U8(0).
		U8(0).
		// +0x24c walk-speed / +0x250 run-speed sources (sub_85fb20).
		F32(float32(walkSpeed)).
		F32(float32(runSpeed)).
		// +0x4d8 scale source: the client computes 100.0/value with an fld
		// dword (FLOAT load, SRO_Client.exe @0x0085fba5), so this must be an
		// f32 100 -> recip 1.0. An integer write would load as a denormal.
		F32(float32(scaleDenom)).
		U8(0).
		U8(1)
	name := []byte(npc.Name)
	w.U16(uint16(len(name))).Bytes(name)
	return w.Payload()
}

// npcPatrolPose converts a PatrolState into the shared wire position.
func npcPatrolPose(state PatrolState) wire.Position {
	return wire.Position{
		RegionID: state.RegionID,
		X:        float32(state.X),
		Y:        float32(state.Y),
		Z:        float32(state.Z),
		Heading:  state.HeadingWord,
	}
}

// NpcMoveFrames is one 0x30E3 source move per roster NPC for the given tick
// (the entity-tick payload the reference npc-tick pull seam served).
func NpcMoveFrames(roster []NpcDef, anchor Spawn, tick int64) []Frame {
	frames := make([]Frame, 0, len(roster))
	for index := range roster {
		if !roster[index].Patrol {
			continue
		}
		npcAnchor := anchor
		npcAnchor.X += roster[index].SpawnOffsetX
		npcAnchor.Z += roster[index].SpawnOffsetZ
		state := ComputeNpcPatrolState(npcAnchor, tick)
		move := wire.ObjectSourceMove{
			Position: npcPatrolPose(state),
			Gid:      roster[index].ObjectID,
		}
		frames = append(frames, Frame{Opcode: wire.OpObjectSourceMove, Payload: move.Encode()})
	}
	return frames
}

// NpcCorrectionFrames is one 0xB2F5 source-position correction per roster NPC
// for the given tick: the same authoritative patrol state, so a correction
// snaps the remote entity onto the server's canonical track (the lenient
// PathCtl re-seed the native runs).
func NpcCorrectionFrames(roster []NpcDef, anchor Spawn, tick int64) []Frame {
	frames := make([]Frame, 0, len(roster))
	for index := range roster {
		if !roster[index].Patrol {
			continue
		}
		npcAnchor := anchor
		npcAnchor.X += roster[index].SpawnOffsetX
		npcAnchor.Z += roster[index].SpawnOffsetZ
		state := ComputeNpcPatrolState(npcAnchor, tick)
		correction := wire.ObjectSourceCorrection{
			Gid:      roster[index].ObjectID,
			Position: npcPatrolPose(state),
		}
		frames = append(frames, Frame{Opcode: wire.OpObjectSourceCorrection, Payload: correction.Encode()})
	}
	return frames
}

// NpcMoveStateFrames is one 0x3122 run/walk refresh per roster NPC: run on
// odd ticks, walk on even ticks (sub_858450 mode 3 = run, 2 = walk), so the
// packet is a real state CHANGE. On-demand exercise seam in the reference
// (npc-state pull), not part of the periodic entity tick.
func NpcMoveStateFrames(roster []NpcDef, tick int64) []Frame {
	value := wire.MoveStateWalk
	if tick&1 == 1 {
		value = wire.MoveStateRun
	}
	return npcMoveStateFrames(roster, wire.StateChannelMove, value)
}

func npcMoveStateFrames(roster []NpcDef, channel, value uint8) []Frame {
	frames := make([]Frame, 0, len(roster))
	for index := range roster {
		if !roster[index].Patrol {
			continue
		}
		refresh := wire.ObjectStateRefresh{
			Gid:       roster[index].ObjectID,
			StateType: channel,
			Value:     value,
		}
		frames = append(frames, Frame{Opcode: wire.OpObjectStateRefresh, Payload: refresh.Encode()})
	}
	return frames
}

// NpcLifeStateFrames is one 0x3122 life refresh per roster NPC: revive on odd
// ticks, kill on even ticks (sub_777b60 life value 1/2), exercising both real
// timer paths (death arms sub_a00b30, revive clears via sub_a00980).
// On-demand exercise seam like NpcMoveStateFrames.
func NpcLifeStateFrames(roster []NpcDef, tick int64) []Frame {
	value := wire.LifeStateDead
	if tick&1 == 1 {
		value = wire.LifeStateAlive
	}
	return npcStateFrames(roster, wire.StateChannelLife, value)
}

func npcStateFrames(roster []NpcDef, channel, value uint8) []Frame {
	frames := make([]Frame, 0, len(roster))
	for index := range roster {
		if !roster[index].Patrol {
			continue
		}
		refresh := wire.ObjectStateRefresh{
			Gid:       roster[index].ObjectID,
			StateType: channel,
			Value:     value,
		}
		frames = append(frames, Frame{Opcode: wire.OpObjectStateRefresh, Payload: refresh.Encode()})
	}
	return frames
}

// NpcDespawnFrames is one 0x36AB despawn per roster NPC (the same 4-byte gid
// wire the pickup path sends; sub_777310 -> vt+0x4c). The trigger seam is the
// caller's - session exit or out-of-range removal.
func NpcDespawnFrames(roster []NpcDef) []Frame {
	frames := make([]Frame, 0, len(roster))
	for index := range roster {
		despawn := wire.ObjectDespawn{Gid: roster[index].ObjectID}
		frames = append(frames, Frame{Opcode: wire.OpObjectDespawn, Payload: despawn.Encode()})
	}
	return frames
}
