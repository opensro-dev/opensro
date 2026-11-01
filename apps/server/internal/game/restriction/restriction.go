// Package restriction projects enabled native session restrictions onto the
// client wire. Each caller owns whether to return, rewrite, or continue afterward.
package restriction

import (
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/transport"
)

// Report mirrors the server's enabled-record test and 36EA notification (research
// opcode 3405). Do not infer expiration from the server clock or add a GM bypass.
// A failed send still returns true: transport failure cannot authorize an action.
func Report(s *transport.Session, kind uint8) bool {
	var channel byte
	switch kind {
	case transport.CommandRestrictionChat:
		channel = 0
	case transport.CommandRestrictionTrade:
		channel = 1
	default:
		return false
	}
	until, enabled := s.CommandRestriction(kind)
	if enabled {
		_ = s.Send(wire.OpGMRestrictionNotice, wire.EncodeGMRestrictionNotice(channel, until))
	}
	return enabled
}
