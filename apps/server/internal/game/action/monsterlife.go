package action

import "opensro.online/server/internal/game/item/wire"

// Native mob vtable +1F0 publishes LIFE for an
// alive-to-dead transition. v1.188's 30BF maps to v1.150's 3122.
// Call only after the HP transaction commits; no second HP debit or fabricated
// movement stop is needed (natively +4B0 runs with broadcast=false).
func monsterLifeDeadFrame(gid uint32) wire.Frame {
	return wire.Frame{Opcode: wire.OpObjectStateRefresh, Payload: (wire.ObjectStateRefresh{
		Gid: gid, StateType: wire.StateChannelLife, Value: wire.LifeStateDead,
	}).Encode()}
}
