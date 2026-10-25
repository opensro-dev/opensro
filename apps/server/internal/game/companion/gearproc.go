/*
===========================================================================
gearproc.go - separate striking/struck blues, powered by their worn degree
===========================================================================
*/
package companion

import "strings"

const (
	ProcChain = iota
	ProcBlizzard
	ProcFire
	ProcPoison
	ProcMend
	ProcCount
)

/*
================
GearProc

Chances add across equipment; power uses the strongest contributing item.
================
*/
type GearProc struct {
	Chance float64
	Damage uint32
}

/*
================
addProc
================
*/
func (m *Modifiers) addProc(group string, chance float64, damage uint32) bool {
	trigger := 0
	if strings.HasSuffix(group, "Taken") {
		trigger = 1
		group = strings.TrimSuffix(group, "Taken")
	}
	kind := -1
	switch group {
	case "ProcChain":
		kind = ProcChain
	case "ProcBlizzard":
		kind = ProcBlizzard
	case "ProcFire":
		kind = ProcFire
	case "ProcPoison":
		kind = ProcPoison
	case "ProcMend":
		kind = ProcMend
	}
	if kind < 0 {
		return false
	}
	p := &m.Procs[kind][trigger]
	p.Chance = min(100, p.Chance+chance)
	if chance > 0 {
		p.Damage = max(p.Damage, damage)
	}
	return true
}
