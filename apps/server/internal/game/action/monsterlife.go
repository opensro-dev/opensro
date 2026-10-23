package action

import "opensro.online/server/internal/game/item/wire"

// The server publishes LIFE for an alive-to-dead transition on the
// v1.150 entity-state opcode (3122). Call only after the HP transaction
// commits; no second HP debit or fabricated movement stop is needed
// (the native death path stops movement without broadcasting it).
func monsterLifeDeadFrame(gid uint32) wire.Frame {
	return wire.Frame{Opcode: wire.OpObjectStateRefresh, Payload: (wire.ObjectStateRefresh{
		Gid: gid, StateType: wire.StateChannelLife, Value: wire.LifeStateDead,
	}).Encode()}
}
