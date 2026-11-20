/*
===========================================================================

server_event.go - the 0x34F2 server event messages (Server Mission gauge)

The v1.188 game server relays the shard manager's server event state to
its clients as 0x34F2, the same id as the shard manager's message; the
later v1.696 client reads it (client 0x00CFA790) and draws
CNIFServerEvent from it. The v1.150
client has no reader, so only the port's opt-in gauge consumes these.
Byte 0 is the message type; the bodies here are the ones the winter
missions need:

  2/3 enter game   u32 N, N x {u32 ID, u32 Count, u32 Goal}
  2/1 sync         u32 N, N x {u32 ID, u32 Count}             (every 300 s)
  1   completion   u32 ID, u8 target, u8 data, u32 Count, u32 Goal,
                   u32 RewardID, u8 class, ExpUP body
  5   start        u32 ID, u8 flag, u32 Goal
  6   end          u32 ID

===========================================================================
*/

package wire

// OpServerEvent is the server event message, the same id as the shard
// manager's message the game server relays.
const OpServerEvent uint16 = 0x34F2

// Message types (byte 0) and their sub-kinds.
const (
	serverEventCompletion uint8 = 1
	serverEventSync       uint8 = 2
	serverEventStart      uint8 = 5
	serverEventEnd        uint8 = 6

	serverEventSyncCount uint8 = 1 // ID, Count
	serverEventSyncFull  uint8 = 3 // ID, Count, Goal

	// The reference server accepts targets 1 (every player) and 2 (a
	// named winner); it logs any other value as not supported.
	serverEventTargetAll uint8 = 1
	// DataType 3: the completion carries the carried-over count and
	// the next goal (client FUN_0080F4E0).
	serverEventDataCountGoal uint8 = 3
	// _RefServerEventReward class 1: ExpUPForPlayers.
	serverEventRewardExpUp uint8 = 1
	// Type 5 flag: a goal follows and the window opens at zero.
	serverEventStartWithGoal uint8 = 1
)

/*
================
ServerEventRow

One mission's progress as the messages carry it.
================
*/
type ServerEventRow struct {
	ID    uint32
	Count uint32
	Goal  uint32
}

/*
================
ServerEventExpUp

The ExpUPForPlayers reward a completion announces. Exp and Sp are ratios
(0.2 for +20%): the v1.696 client multiplies them by 100.0 (0x013BF60C)
before it prints or stores them; the reference row
(_RefServerEventReward_ExpUPForPlayers 0x1ED) holds 3600 s and 20/20.
================
*/
type ServerEventExpUp struct {
	RewardID     uint32
	ApplySeconds uint32
	Exp          float32
	Sp           float32
}

/*
================
ServerEventFullSyncFrame

Every row with its goal: what a character entering the world gets.
================
*/
func ServerEventFullSyncFrame(rows []ServerEventRow) Frame {
	w := NewWriter(6 + 12*len(rows)).U8(serverEventSync).U8(serverEventSyncFull).U32(uint32(len(rows)))
	for _, row := range rows {
		w.U32(row.ID).U32(row.Count).U32(row.Goal)
	}
	return Frame{Opcode: OpServerEvent, Payload: w.Payload()}
}

/*
================
ServerEventCountSyncFrame

The counts alone; the client keeps the goal it already has.
================
*/
func ServerEventCountSyncFrame(rows []ServerEventRow) Frame {
	w := NewWriter(6 + 8*len(rows)).U8(serverEventSync).U8(serverEventSyncCount).U32(uint32(len(rows)))
	for _, row := range rows {
		w.U32(row.ID).U32(row.Count)
	}
	return Frame{Opcode: OpServerEvent, Payload: w.Payload()}
}

/*
================
ServerEventCompletionFrame

A completion sent to every player: the count carried over, the next goal
and the EXP bonus it pays (the v1.188 ExpUP body: time, EXP, SP).
================
*/
func ServerEventCompletionFrame(row ServerEventRow, reward ServerEventExpUp) Frame {
	w := NewWriter(32).U8(serverEventCompletion).U32(row.ID).U8(serverEventTargetAll).U8(serverEventDataCountGoal)
	w.U32(row.Count).U32(row.Goal)
	w.U32(reward.RewardID).U8(serverEventRewardExpUp).U32(reward.ApplySeconds).F32(reward.Exp).F32(reward.Sp)
	return Frame{Opcode: OpServerEvent, Payload: w.Payload()}
}

/*
================
ServerEventStartFrame

The event started: the mission opens at zero towards its goal (the
client sets the row with a zero count whatever the server holds).
================
*/
func ServerEventStartFrame(row ServerEventRow) Frame {
	w := NewWriter(10).U8(serverEventStart).U32(row.ID).U8(serverEventStartWithGoal).U32(row.Goal)
	return Frame{Opcode: OpServerEvent, Payload: w.Payload()}
}

/*
================
ServerEventEndFrame

The event ended: the client closes the window.
================
*/
func ServerEventEndFrame(id uint32) Frame {
	return Frame{Opcode: OpServerEvent, Payload: NewWriter(5).U8(serverEventEnd).U32(id).Payload()}
}
