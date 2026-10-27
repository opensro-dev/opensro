// Package gmcommand is the GM command lane of the v1.150 gateway: the C->S
// 0x75B6 GM command request (the client's GmCommand_ProcessChatSlashCommand
// composer, sub_509cf0 @0x00509cf0) and its S->C 0xB5B6 acknowledgement
// (the client's sub_751ec0 @0x00751ec0 consumer).
//
// Every opcode and byte layout here is pinned from the v1.150 CLIENT folds
// (the source of truth for what it composes/parses), never invented. The
// client composes ONE 0x75B6 frame per slash line: a subcommand byte plus a
// per-command payload. This lane decodes that frame, gates the sender on the
// GMPrivilege flag (bit 0 of the u8 the client mirrors into CICPlayer+0x1890),
// honors the commands the server can actually execute with existing
// capabilities, and REFUSES the rest with the pinned 0xB5B6 result-2 ack -
// the faithful posture: a retail v1.150 client composes commands the server
// declines, and the per-action privilege vfunc in the v1.188 dump
// (*(arg1+0x634))(actionCode) could not be resolved, so no privilege LEVELS
// are invented - the boolean GMPrivilege is the sole gate.
//
// Server-side, FINDUSER, MAKEITEM, coordinate warp, the town and player
// teleports and the action-owned body toggles are implemented. Commands without a production authority remain refused.
package gmcommand

import (
	"encoding/binary"
	"fmt"
	"math"

	"opensro.online/server/internal/game/item/wire"
)

// Opcodes, pinned from the v1.150 client.
const (
	// OpGmCommand is the C->S GM command request: sub_509cf0 builds one
	// 0x75B6 buffer (@0x00509e33 var_4d8_4 = 0x75b6) with a subcommand byte
	// + per-command payload and submits it once (@0x0050cc8d, gated on the
	// internal var_4bb "did match" flag).
	OpGmCommand uint16 = 0x75B6
	// OpGmCommandAck is the S->C acknowledgement, handler sub_751ec0
	// @0x00751ec0: u8 result (1 = OK, 2 = FAIL), u8 subcmd (the echoed
	// outgoing subcommand), then a per-subcmd body.
	OpGmCommandAck uint16 = 0xB5B6
)

// Subcommand bytes, pinned from sub_509cf0's per-command var_4bc/var_4ba/
// var_4b9 stores. Only the ones this lane names are listed; the client
// composes the full table.
const (
	SubInvisible  uint8 = 0x0E
	SubInvincible uint8 = 0x0F
	SubMakeItem   uint8 = 0x07
	// SubLoadMonster: /LOADMONSTER (@0x0050a549 subcmd 6) - u32 refObjID,
	// u8 count, u8 type (CHAMP 1, GIANT 4, NORMAL 0, else the record's +0xA0).
	SubLoadMonster uint8 = 0x06
	SubWarp        uint8 = 0x10
	// SubMobKill: /MOBKILL (@0x0050b754 var_4bc = 0x14, three builder calls
	// of 1, 4 and 2 bytes) - subcmd, u32 selected target id, u16 mode. The
	// client sends it only for a mode argument of exactly 0 or 1.
	SubMobKill uint8 = 0x14
	// SubZoe: /ZOE (@0x0050a6dd var_4bc = 0x0C, three builder calls of 1, 4
	// and 1 bytes) - subcmd, u32 monster refObjID, u8 count (client clamps
	// 1..255). The server loads the monsters and kills them for rewards.
	SubZoe uint8 = 0x0C
	// SubSetTime: /SETTIME hour - subcmd + u8 hour 0..23 (509CF0 clamps;
	// the server's GM handler, case 8, relays it to the shard manager as
	// SetGameTime).
	SubSetTime uint8 = 0x0A
	// SubWorldStatus: /WORLDSTATUS - subcmd alone (GM handler case 3).
	SubWorldStatus uint8 = 0x04
	// SubBan: /BAN - subcmd + ANSI name (GM handler case 0xA).
	SubBan uint8 = 0x0D
	// SubRecallGuild: /RECALLGUILD - subcmd + ANSI guild name (case 0xE).
	SubRecallGuild uint8 = 0x12
	// SubGoTown: /GOTOWN - subcmd alone. The server answers it in
	// v1.188 per-player GM special-command handler, case 1.
	SubGoTown uint8 = 0x02
	// SubToTown: /TOTOWN - subcmd + ANSI name (GM handler case 2).
	SubToTown uint8 = 0x03
	// SubMoveToUser: /MOVETOUSER - subcmd + ANSI name (GM handler case 7).
	SubMoveToUser uint8 = 0x08
	// SubRecallUser: /RECALLUSER - subcmd + ANSI name (GM handler case
	// 0xD, relayed to the shard's GM command processor).
	SubRecallUser uint8 = 0x11
	// SubFindUser: /FINDUSER (@0x00509ee5 var_4bc = 1) - subcmd + ANSI name.
	SubFindUser uint8 = 0x01
	// SubLieName: /LIENAME (@0x0050bcf3 var_4bc = 0x19) - subcmd + ANSI name.
	SubLieName uint8 = 0x19
	// SubRealName: /REALNAME (@0x0050bbd1 var_4bc = 0x1a) - subcmd + ANSI name.
	SubRealName uint8 = 0x1A
	// The quest tools (509CF0 50CA69..50CBE6): the discriminator byte, then
	// for all but /RESETQ a sized ANSI quest codename (4C4350 then 4FD5B0).
	SubInitQ   uint8 = 0x1B
	SubResetQ  uint8 = 0x1C
	SubCompQ   uint8 = 0x1D
	SubRemoveQ uint8 = 0x1E
	// SubOpenWorld: /INSTANCE worldId layerId (client 50B56C..50B627; the server's
	// GM handler opens the world).
	SubOpenWorld uint8 = 0x13
	// SubMoveToNPC: /movetonpc codename (client 50C982..50CA57; the
	// server's GM handler moves to the NPC).
	SubMoveToNPC uint8 = 0x1F
	// SubSiegeStructure: /SIEGE STRUCT_BUILD|STRUCT_UPGRADE|STRUCT_REPAIR|GATE
	// (client 50C2FD..50C66B; the server's GM handler forwards to its siege
	// structure command).
	SubSiegeStructure uint8 = 0x20
	// SubSiegePeriod: /SIEGE STARTWAR|ENDWAR|STARTTAX|ENDTAX|STARTREQ|ENDREQ
	// (client 50C1DF..50C2DC; the server's GM handler relays to the fortress
	// manager's shard message).
	SubSiegePeriod uint8 = 0x21
	// SubFlagWorld: /flagworld n (client 50C88F..50C971; the server's GM
	// handler sets the flag).
	SubFlagWorld uint8 = 0x30
	// SubGrantSilk is the port's /SILK name amount (operator tooling, not
	// native): subcmd, u16 len + ANSI name, u32 amount. The native table
	// ends at 0x30, so 0xF0 cannot collide with a retail command.
	SubGrantSilk uint8 = 0xF0
	// SubSetSpeed is the port's /SETSPEED percent (GM tooling, not native,
	// action/gmspeed.go): subcmd, u16 percent of the effective speed.
	SubSetSpeed uint8 = 0xF1
)

// Siege structure sub-actions for SubSiegeStructure (0x20) and period codes for
// SubSiegePeriod (0x21), pinned from client
// 50C1DF..50C66B and the server's siege command and period relay.
const (
	SiegeActionBuild   uint8 = 0x0A
	SiegeActionUpgrade uint8 = 0x0B
	SiegeActionRepair  uint8 = 0x0C
	SiegeActionGate    uint8 = 0x15

	SiegePeriodStartWar uint8 = 1
	SiegePeriodEndWar   uint8 = 2
	SiegePeriodStartTax uint8 = 3
	SiegePeriodEndTax   uint8 = 4
	SiegePeriodStartReq uint8 = 5
	SiegePeriodEndReq   uint8 = 6
)

// Ack result bytes (sub_751ec0 @0x00751f08 result == 1 / @0x00752111
// result == 2).
const (
	AckResultOK   uint8 = 1
	AckResultFail uint8 = 2
)

// GmCommandNameLenBound mirrors the community lane's name bound: a name
// length >= 0x80 is rejected before any lookup (a non-retail frame).
const GmCommandNameLenBound = 0x80

// Request is one decoded 0x75B6 frame: the subcommand byte plus, for the
// name-payload commands, the decoded ANSI argument. MAKEITEM has its own
// exact six-byte arm; warp has a seventeen-byte position arm. Unsupported
// numeric commands retain only the discriminator.
type Request struct {
	Subcmd      uint8
	RefObjID    uint32
	Amount      uint8
	Destination wire.Position
	// MonsterType is /LOADMONSTER's requested type byte (the server masks it &0xF).
	MonsterType uint8
	// TargetGID is /MOBKILL's selected monster object id.
	TargetGID uint32
	// MobKillMode is /MOBKILL's mode word: 0 kills with EXP/SP and drops, 1
	// kills without them. INFERENCE: community GM guides for the V-Sro client
	// describe the two modes this way; no server binary was read.
	MobKillMode uint16
	// Name is the decoded ANSI argument for the name-payload subcommands
	// (FINDUSER/LIENAME/REALNAME/...). Empty for other subcommands.
	Name string
	// HasName reports whether Subcmd is a name-payload command this lane
	// decoded a name for.
	HasName bool
	// Silk is /SILK's amount.
	Silk uint32
	// SpeedPercent is /SETSPEED's percent of the effective speed.
	SpeedPercent uint16
	// WorldID and LayerID are /INSTANCE's (SubOpenWorld 0x13) u16 arguments.
	WorldID uint16
	LayerID uint16
	// SiegeAction, FortressID, EventStructID, and SiegeParam are /SIEGE's
	// structure (0x20) and period (0x21) fields.
	SiegeAction   uint8
	FortressID    uint32
	EventStructID uint32
	SiegeParam    uint8
}

// nameSubcommands are the subcommands whose payload is a single sized ANSI
// name (subcmd byte then sub_4fd5b0 u16 len + bytes). This lane decodes the
// name for exactly these; MAKEITEM is decoded independently below.
var nameSubcommands = map[uint8]bool{
	SubFindUser:    true,
	SubToTown:      true,
	SubMoveToUser:  true,
	SubBan:         true,
	SubRecallUser:  true,
	SubRecallGuild: true,
	SubLieName:     true,
	SubRealName:    true,
	SubInitQ:       true,
	SubCompQ:       true,
	SubRemoveQ:     true,
	SubMoveToNPC:   true,
	// SubGrantSilk's name is followed by the u32 amount.
	SubGrantSilk: true,
}

// DecodeGmCommand reads the subcommand byte and, for the name-payload
// commands, the sized ANSI argument. It never trusts the client past the
// bounds it can prove: a name length >= 0x80 or a short frame is an error
// (the lane's malformed-frame posture). Unsupported numeric commands decode to
// {Subcmd, HasName:false} and are refused without interpreting their payloads.
func DecodeGmCommand(payload []byte) (Request, error) {
	reader := wire.NewReader(payload)
	subcmd, err := reader.U8()
	if err != nil {
		return Request{}, err
	}
	request := Request{Subcmd: subcmd}
	if subcmd == SubWarp {
		if len(payload) != 17 {
			return Request{}, fmt.Errorf("gmcommand: warp requires seventeen bytes")
		}
		p := wire.Position{RegionID: binary.LittleEndian.Uint16(payload[1:]), X: math.Float32frombits(binary.LittleEndian.Uint32(payload[3:])), Y: math.Float32frombits(binary.LittleEndian.Uint32(payload[7:])), Z: math.Float32frombits(binary.LittleEndian.Uint32(payload[11:])), Heading: binary.LittleEndian.Uint16(payload[15:])}
		if p.RegionID == 0 || math.IsNaN(float64(p.X)) || math.IsNaN(float64(p.Y)) || math.IsNaN(float64(p.Z)) || math.IsInf(float64(p.X), 0) || math.IsInf(float64(p.Y), 0) || math.IsInf(float64(p.Z), 0) {
			return Request{}, fmt.Errorf("gmcommand: invalid warp position")
		}
		request.Destination = p
		return request, nil
	}
	if subcmd == SubSetSpeed {
		if len(payload) != 3 {
			return Request{}, fmt.Errorf("gmcommand: SETSPEED requires three bytes")
		}
		request.SpeedPercent, err = reader.U16()
		return request, err
	}
	if subcmd == SubMakeItem {
		if len(payload) != 6 {
			return Request{}, fmt.Errorf("gmcommand: MAKEITEM requires six bytes")
		}
		request.RefObjID, err = reader.U32()
		if err != nil {
			return Request{}, err
		}
		request.Amount, err = reader.U8()
		return request, err
	}
	if subcmd == SubSetTime {
		if len(payload) != 2 || payload[1] >= 24 {
			return Request{}, fmt.Errorf("gmcommand: SETTIME requires one hour byte below 24")
		}
		request.Amount = payload[1]
		return request, nil
	}
	if subcmd == SubLoadMonster {
		// 50A53E..50A57E: subcmd, u32 refObjID, u8 count, u8 type.
		if len(payload) != 7 {
			return Request{}, fmt.Errorf("gmcommand: LOADMONSTER requires seven bytes")
		}
		request.RefObjID, err = reader.U32()
		if err != nil {
			return Request{}, err
		}
		if request.Amount, err = reader.U8(); err != nil {
			return Request{}, err
		}
		request.MonsterType, err = reader.U8()
		return request, err
	}
	if subcmd == SubMobKill {
		// 50B754..50B77E: subcmd, u32 target id, u16 mode.
		if len(payload) != 7 {
			return Request{}, fmt.Errorf("gmcommand: MOBKILL requires seven bytes")
		}
		if request.TargetGID, err = reader.U32(); err != nil {
			return Request{}, err
		}
		if request.MobKillMode, err = reader.U16(); err != nil {
			return Request{}, err
		}
		if request.MobKillMode > 1 {
			return Request{}, fmt.Errorf("gmcommand: MOBKILL mode %d is not 0 or 1", request.MobKillMode)
		}
		return request, nil
	}
	if subcmd == SubZoe {
		// 50A6DD..50A707: subcmd, u32 refObjID, u8 count.
		if len(payload) != 6 {
			return Request{}, fmt.Errorf("gmcommand: ZOE requires six bytes")
		}
		if request.RefObjID, err = reader.U32(); err != nil {
			return Request{}, err
		}
		request.Amount, err = reader.U8()
		return request, err
	}
	if subcmd == SubResetQ {
		if len(payload) != 1 {
			return Request{}, fmt.Errorf("gmcommand: RESETQ requires one byte")
		}
		return request, nil
	}
	if subcmd == SubOpenWorld {
		if len(payload) != 5 {
			return Request{}, fmt.Errorf("gmcommand: INSTANCE requires five bytes")
		}
		if request.WorldID, err = reader.U16(); err != nil {
			return Request{}, err
		}
		request.LayerID, err = reader.U16()
		return request, err
	}
	if subcmd == SubSiegeStructure {
		if len(payload) < 10 {
			return Request{}, fmt.Errorf("gmcommand: SIEGE structure requires at least ten bytes")
		}
		if request.SiegeAction, err = reader.U8(); err != nil {
			return Request{}, err
		}
		if request.FortressID, err = reader.U32(); err != nil {
			return Request{}, err
		}
		if request.EventStructID, err = reader.U32(); err != nil {
			return Request{}, err
		}
		switch request.SiegeAction {
		case SiegeActionBuild, SiegeActionRepair:
			if len(payload) != 10 {
				return Request{}, fmt.Errorf("gmcommand: SIEGE structure action %#x requires ten bytes", request.SiegeAction)
			}
		case SiegeActionUpgrade, SiegeActionGate:
			if len(payload) != 11 {
				return Request{}, fmt.Errorf("gmcommand: SIEGE structure action %#x requires eleven bytes", request.SiegeAction)
			}
			if request.SiegeParam, err = reader.U8(); err != nil {
				return Request{}, err
			}
		default:
			return Request{}, fmt.Errorf("gmcommand: unsupported SIEGE structure action %#x", request.SiegeAction)
		}
		return request, nil
	}
	if subcmd == SubSiegePeriod {
		if len(payload) != 2 {
			return Request{}, fmt.Errorf("gmcommand: SIEGE period requires two bytes")
		}
		if request.SiegeAction, err = reader.U8(); err != nil {
			return Request{}, err
		}
		if request.SiegeAction < SiegePeriodStartWar || request.SiegeAction > SiegePeriodEndReq {
			return Request{}, fmt.Errorf("gmcommand: invalid SIEGE period action %d", request.SiegeAction)
		}
		return request, nil
	}
	if subcmd == SubFlagWorld {
		if len(payload) != 2 {
			return Request{}, fmt.Errorf("gmcommand: flagworld requires two bytes")
		}
		request.Amount, err = reader.U8()
		return request, err
	}
	if !nameSubcommands[subcmd] {
		return request, nil
	}
	nameLen, err := reader.U16()
	if err != nil {
		return Request{}, err
	}
	if nameLen >= GmCommandNameLenBound {
		return Request{}, fmt.Errorf("gmcommand: name length %d breaches the 0x80 bound", nameLen)
	}
	nameBytes, err := reader.Bytes(int(nameLen))
	if err != nil {
		return Request{}, err
	}
	request.Name = string(nameBytes)
	request.HasName = true
	if (subcmd == SubInitQ || subcmd == SubCompQ || subcmd == SubRemoveQ || subcmd == SubMoveToNPC) && len(payload) != 3+int(nameLen) {
		return Request{}, fmt.Errorf("gmcommand: trailing quest-tool or movetonpc bytes")
	}
	if subcmd == SubGrantSilk {
		if request.Silk, err = reader.U32(); err != nil {
			return Request{}, err
		}
		if len(payload) != 3+int(nameLen)+4 {
			return Request{}, fmt.Errorf("gmcommand: trailing /SILK bytes")
		}
	}
	return request, nil
}

// EncodeAckGuide renders the 0xB5B6 result-1 body for a subcommand whose OK
// arm carries a guide string: {0x01, subcmd, u16 len + ANSI text}. FINDUSER
// (subcmd 0x01) lands in sub_751ec0's case-0 guide-message arm (@0x00751f3d).
func EncodeAckGuide(subcmd uint8, text string) []byte {
	textBytes := []byte(text)
	writer := wire.NewWriter(4 + len(textBytes))
	writer.U8(AckResultOK)
	writer.U8(subcmd)
	writer.U16(uint16(len(textBytes)))
	writer.Bytes(textBytes)
	return writer.Payload()
}

// EncodeAckFail renders the 0xB5B6 result-2 body {0x02, subcmd}: sub_751ec0's
// result-2 default arm (@0x00752148) shows a generic system message keyed by
// the subcommand code. This is the faithful refusal for every command this
// server cannot honor.
func EncodeAckFail(subcmd uint8) []byte {
	return []byte{AckResultFail, subcmd}
}

// EncodeRequestFailure retains the two alias-command failure payloads consumed
// by native 751EC0: sized name followed by one ignored reason byte.
func EncodeRequestFailure(request Request) []byte {
	if request.Subcmd == SubLieName || request.Subcmd == SubRealName {
		result := EncodeAckGuide(request.Subcmd, request.Name)
		result[0] = AckResultFail
		return append(result, 0)
	}
	return EncodeAckFail(request.Subcmd)
}
