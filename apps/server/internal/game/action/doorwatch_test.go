/*
===========================================================================

doorwatch_test.go - every write to a character record happens in its door

A door holds the store's write lock and marks the records it names dirty.
A write made between doors, or during a door that does not name the
record, races every reader under the read door (ReadState, Snapshot), and
when the field is persisted (the record's JSON, store db_write.go) the
next restart loses it (#1836).

doorWatch wraps every character door seam and compares each watched
record, field by field, at every door's entry and exit. It checks what the
code does to the record, not which door it uses or what the door is
called, so a test built on it breaks only when a write escapes its door.

===========================================================================
*/

package action

import (
	"reflect"
	"slices"
	"testing"

	"opensro.online/server/internal/game/enterworld"
)

/*
================
doorWatch
================
*/
type doorWatch struct {
	t       *testing.T
	watched []*enterworld.Character
	seen    map[*enterworld.Character]*enterworld.Character
	covered map[*enterworld.Character]int
	depth   int
}

/*
================
watchDoors

Wrap every door seam of rt so each door reports the watched records'
changes. A nil seam keeps its fallback: the wrapper runs the callback
directly, as the detached composition does.
================
*/
func watchDoors(t *testing.T, rt *Runtime, watched ...*enterworld.Character) *doorWatch {
	t.Helper()
	w := &doorWatch{t: t, watched: watched, seen: map[*enterworld.Character]*enterworld.Character{}, covered: map[*enterworld.Character]int{}}
	for _, c := range watched {
		w.seen[c] = c.Snapshot()
	}
	deps := rt.deps.(*enterworld.Deps)
	// The fallbacks of enterworld deps.go, kept under the wrapper with
	// their locks: a nil Update seam commits through Mutate, and a nil
	// deferred seam through the immediate one.
	if mutate := deps.MutateCharacter; deps.UpdateCharacter == nil && mutate != nil {
		deps.UpdateCharacter = func(c *enterworld.Character, label string, update func() bool) bool {
			changed := false
			mutate(c, label, func() { changed = update() })
			return changed
		}
	}
	if mutate := deps.MutateCharacters; deps.UpdateCharacters == nil && mutate != nil {
		deps.UpdateCharacters = func(cs []*enterworld.Character, label string, update func() bool) bool {
			changed := false
			mutate(cs, label, func() { changed = update() })
			return changed
		}
	}
	if deps.UpdateCharacterDeferred == nil {
		deps.UpdateCharacterDeferred = deps.UpdateCharacter
	}
	if deps.UpdateCharactersDeferred == nil {
		deps.UpdateCharactersDeferred = deps.UpdateCharacters
	}
	if deps.MutateCharacterDeferred == nil {
		deps.MutateCharacterDeferred = deps.MutateCharacter
	}
	if deps.MutateCharactersDeferred == nil {
		deps.MutateCharactersDeferred = deps.MutateCharacters
	}
	deps.UpdateCharacterDeferred = w.wrapUpdate(deps.UpdateCharacterDeferred)
	deps.UpdateCharactersDeferred = w.wrapUpdateMany(deps.UpdateCharactersDeferred)
	deps.MutateCharacterDeferred = w.wrapMutate(deps.MutateCharacterDeferred)
	deps.MutateCharactersDeferred = w.wrapMutateMany(deps.MutateCharactersDeferred)
	deps.UpdateCharacter = w.wrapUpdate(deps.UpdateCharacter)
	deps.UpdateCharacters = w.wrapUpdateMany(deps.UpdateCharacters)
	deps.MutateCharacter = w.wrapMutate(deps.MutateCharacter)
	deps.MutateCharacters = w.wrapMutateMany(deps.MutateCharacters)
	return w
}

/*
================
door

One door over covered: a watched change found on entry happened outside
any door; on exit, a covered record's change is the door's own and any
other watched record must be unchanged.
================
*/
func (w *doorWatch) door(covered []*enterworld.Character, run func() bool) bool {
	if w.depth == 0 {
		w.settle("before a door")
	}
	w.depth++
	ok := run()
	w.depth--
	for _, c := range w.watched {
		now := c.Snapshot()
		if reflect.DeepEqual(now, w.seen[c]) {
			continue
		}
		if slices.Contains(covered, c) {
			w.covered[c]++
		} else {
			w.t.Errorf("%s's record changed during a door that does not cover it", c.Name)
		}
		w.seen[c] = now
	}
	return ok
}

/*
================
settle

Report every watched record that changed since the last door closed,
then accept it, so one escape is reported once.
================
*/
func (w *doorWatch) settle(where string) {
	for _, c := range w.watched {
		if now := c.Snapshot(); !reflect.DeepEqual(now, w.seen[c]) {
			w.t.Errorf("%s's record changed outside any door (seen %s)", c.Name, where)
			w.seen[c] = now
		}
	}
}

/*
================
finish

The end of a step: catch a write made after the last door, and return how
many doors changed each watched record since the previous finish.
================
*/
func (w *doorWatch) finish() map[*enterworld.Character]int {
	w.settle("at the end of the step")
	covered := w.covered
	w.covered = map[*enterworld.Character]int{}
	return covered
}

/*
================
wrapUpdate
================
*/
func (w *doorWatch) wrapUpdate(inner func(*enterworld.Character, string, func() bool) bool) func(*enterworld.Character, string, func() bool) bool {
	return func(c *enterworld.Character, label string, update func() bool) bool {
		return w.door([]*enterworld.Character{c}, func() bool {
			if inner != nil {
				return inner(c, label, update)
			}
			return update()
		})
	}
}

/*
================
wrapUpdateMany
================
*/
func (w *doorWatch) wrapUpdateMany(inner func([]*enterworld.Character, string, func() bool) bool) func([]*enterworld.Character, string, func() bool) bool {
	return func(cs []*enterworld.Character, label string, update func() bool) bool {
		return w.door(cs, func() bool {
			if inner != nil {
				return inner(cs, label, update)
			}
			return update()
		})
	}
}

/*
================
wrapMutate
================
*/
func (w *doorWatch) wrapMutate(inner func(*enterworld.Character, string, func())) func(*enterworld.Character, string, func()) {
	return func(c *enterworld.Character, label string, mutate func()) {
		w.door([]*enterworld.Character{c}, func() bool {
			if inner != nil {
				inner(c, label, mutate)
			} else {
				mutate()
			}
			return true
		})
	}
}

/*
================
wrapMutateMany
================
*/
func (w *doorWatch) wrapMutateMany(inner func([]*enterworld.Character, string, func())) func([]*enterworld.Character, string, func()) {
	return func(cs []*enterworld.Character, label string, mutate func()) {
		w.door(cs, func() bool {
			if inner != nil {
				inner(cs, label, mutate)
			} else {
				mutate()
			}
			return true
		})
	}
}
