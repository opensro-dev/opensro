package monster

import (
	"encoding/binary"
	"fmt"
)

// AICommandKind is the uint16 opcode at the message buffer's +10, dispatched
// by the reference server's pending-AI-message processing. It is
// server-internal; these values are not client wire opcodes.
type AICommandKind uint16

const (
	AICommandHelp            AICommandKind = 1
	AICommandBattle          AICommandKind = 2
	AICommandFlee            AICommandKind = 3
	AICommandRefreshMonsters AICommandKind = 4
)

// AICommand retains a detached, comparable payload. Duration is an unsigned
// state duration, not an absolute deadline or skill ID. Command 4's target is
// only a lookup prerequisite; the native scan runs around the receiving
// actor.
type AICommand struct {
	Kind      AICommandKind
	Help      HelpEvent
	TargetGID uint32
	Duration  uint32
}

func DecodeAICommand(kind AICommandKind, payload []byte) (AICommand, error) {
	command := AICommand{Kind: kind}
	switch kind {
	case AICommandHelp:
		var err error
		command.Help, err = DecodeHelpEvent(payload)
		return command, err
	case AICommandBattle, AICommandFlee:
		if len(payload) != 8 {
			return AICommand{}, fmt.Errorf("AI command %d length %d, want 8", kind, len(payload))
		}
		command.TargetGID = binary.LittleEndian.Uint32(payload)
		command.Duration = binary.LittleEndian.Uint32(payload[4:])
	case AICommandRefreshMonsters:
		if len(payload) != 4 {
			return AICommand{}, fmt.Errorf("AI command 4 length %d, want 4", len(payload))
		}
		command.TargetGID = binary.LittleEndian.Uint32(payload)
	default:
		return AICommand{}, fmt.Errorf("unknown internal AI command %d", kind)
	}
	return command, nil
}

// CommandInbox is the tactics record's +98 slot: every kind replaces the same single slot.
// Consumption leaves the generation unchanged; replacement advances it even
// for an identical payload, so an old planner cannot consume a new command.
type CommandInbox struct {
	command    AICommand
	pending    bool
	generation uint64
}

func (b CommandInbox) ReplaceCommand(command AICommand) CommandInbox {
	b.command, b.pending = command, true
	b.generation++
	return b
}

func (b CommandInbox) PendingCommand() (AICommand, bool) { return b.command, b.pending }
func (b CommandInbox) HasPending() bool                  { return b.pending }
func (b CommandInbox) Consumed() CommandInbox {
	b.command, b.pending = AICommand{}, false
	return b
}

// The existing help ingress uses this same owner; it does not get a second
// slot or priority over later commands of another kind.
func (b CommandInbox) Replace(event HelpEvent) CommandInbox {
	return b.ReplaceCommand(AICommand{Kind: AICommandHelp, Help: event})
}
func (b CommandInbox) Pending() (HelpEvent, bool) {
	return b.command.Help, b.pending && b.command.Kind == AICommandHelp
}

// The native state-deadline check: zero disables expiry; elapsed
// subtraction wraps at uint32 and the comparison is strict. Keep this
// separate from signed wall-clock deadlines.
func NativeStateDurationExpired(started, duration, now uint32) bool {
	return duration != 0 && now-started > duration
}
