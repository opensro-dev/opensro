/*
===========================================================================

cospublication.go - publish mounted movement before its terminal event

Reuses normal action admission and delivery. Only the movement branch
publishes inside the mover's operation lock, before a tick can stop it.

===========================================================================
*/
package action

import (
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/transport"
)

/*
================
cosCommandHubHandler
================
*/
func (rt *Runtime) cosCommandHubHandler(hub *transport.Hub) transport.HandlerFunc {
	return func(session *transport.Session, opcode uint16, payload []byte) {
		handle := rt.hubHandler(hub, func(division string, character *enterworld.Character, body []byte) OpResult {
			return rt.handleCosCommand(division, character, body, func(frames []wire.Frame) {
				sendFrames(session, frames)
			})
		})
		handle(session, opcode, payload)
	}
}
