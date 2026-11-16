/*
===========================================================================

offline_hold.go - run a write against a character only while it is out of the world

A door outside the world (the website market's gold buy, opensro-web#148)
may write an offline character's record, but never one an entry is
admitting at the same moment: the entered character's record then
belongs to its action lane. Entry admits under the division operation
lock (AdmitCharacterSession), and so does the session close that ends it
(ForgetCharacterSession), so a write run under that lock after finding
no admission cannot interleave with either. An entry that follows the
write reads the written record: the bootstrap snapshots the character
after PrepareEntry admits it (enterworld/bootstrap.go).

Port-only, not native: the original has no out-of-world door.

===========================================================================
*/
package action

import "opensro.online/server/internal/game/world/simulation"

/*
================
WhileOffline

Runs write under the division operation lock when the named character
holds no world admission, and reports online (write not run) when it
does. The live session list is a published snapshot and can lag an
entry, so it is never the gate.
================
*/
func (rt *Runtime) WhileOffline(division, name string, write func() error) (online bool, err error) {
	unlock := rt.lockDivision(division)
	defer unlock()
	if _, admitted := rt.characterAdmissions.Load(simulation.WorldKey(division, name)); admitted {
		return true, nil
	}
	return false, write()
}
