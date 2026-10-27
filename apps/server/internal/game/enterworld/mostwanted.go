/*
===========================================================================

mostwanted.go - a suited streak holder's wanted state at world entry

Port-only, not native (#1849; the rule is action/mostwanted.go). World
entry re-raises the wanted state beside the other board rows the entry
reset emptied, so a suited character holding a streak sees its row from
the first frame. Nothing is sent with the rule off, unsuited, or without
a streak; every later change is the action lane's.

===========================================================================
*/
package enterworld

import (
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/item/wire"
)

/*
================
mostWantedPackets

The entry wanted-state packet for a suited streak holder, or none. A
streak below the first level still reports: its progress is the client's
to keep, only the row waits for a level.
================
*/
func mostWantedPackets(deps *Deps, character *Character) []Packet {
	if deps == nil || !deps.MostWantedEnabled || character == nil ||
		character.MostWanted == nil || character.MostWanted.Streak <= 0 {
		return nil
	}
	job := DressedJob(character)
	if job == 0 {
		return nil
	}
	level := character.MostWanted.Level()
	return []Packet{NewPacket(wire.OpMostWantedState, wire.EncodeMostWantedState(wire.MostWantedState{
		Level:          uint8(level),
		KillsIntoLevel: uint8(character.MostWanted.KillsIntoLevel()),
		KillsPerLevel:  uint8(domain.MostWantedKillsPerLevel),
		Job:            job,
		DamagePercent:  uint8(domain.MostWantedDamagePercent(level)),
	}))}
}
