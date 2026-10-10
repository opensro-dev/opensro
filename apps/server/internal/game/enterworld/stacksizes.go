/*
===========================================================================

stacksizes.go - operator stack-size overrides (port-only, not native)

v1.150 takes every stack cap from the itemdata MaxStack column: potions
stack 50, elixirs 1. Later official versions raised several of these, and
players asked for the same here. SRO_STACK_SIZES raises the caps of named
item groups; unset or empty is native and leaves every row untouched.

The override is applied once, to the loaded item references, before any
gameplay owner reads them. Every cap reader (Runtime.maxStackFor, the
commerce and GM references, the login and published reference rows the
browser takes its caps from) therefore sees one value, and the browser
can never disagree with the server.

An override only raises: a row whose native cap is already at or above it
keeps its own, so a stored stack never exceeds its row's cap while the
setting is on. Turning the setting off leaves larger stacks in place; the
native merge arithmetic (TransferSlotStack) swaps rather than pours into a
row above its cap, so they stay usable and never lose units.

===========================================================================
*/

package enterworld

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
)

// EnvStackSizes names the operator setting, e.g. "potion=2000,elixir=50".
const EnvStackSizes = "SRO_STACK_SIZES"

// maxStackSize is the wire bound: stack counts are u16.
const maxStackSize = 0xFFFF

/*
================
stackGroup

One configurable family, selected by its itemdata TypeID3/TypeID4 under
TypeID1/2 = 3/3 (stackable ETC). raisesSingles lets the group raise rows
the original authored at cap 1; without it only rows that already stack
are raised, so special single items in a potion family keep their cap.
================
*/
type stackGroup struct {
	name          string
	typeID3       int64
	typeID4s      []int64
	raisesSingles bool
}

// stackGroups are the configurable families. Magic stones (3/3/11/1) and
// attribute stones (3/3/11/2) carry their assimilation value in Plus, so
// they merge only with an equal value (inventory.stackIdentityMatches,
// #583). The 3/3/11/7 magic stones carry none and merge like elixirs.
var stackGroups = []stackGroup{
	{name: "potion", typeID3: 1, typeID4s: []int64{1, 2, 3}},
	{name: "petpotion", typeID3: 1, typeID4s: []int64{4, 9}},
	{name: "elixir", typeID3: 10, typeID4s: []int64{1}, raisesSingles: true},
	{name: "luckypowder", typeID3: 10, typeID4s: []int64{2}},
	{name: "magicstone", typeID3: 11, typeID4s: []int64{1, 7}, raisesSingles: true},
	{name: "attrstone", typeID3: 11, typeID4s: []int64{2}, raisesSingles: true},
}

/*
================
StackSizes

Group name to cap. The zero value is native.
================
*/
type StackSizes map[string]uint16

/*
================
ParseStackSizes

"group=cap" pairs separated by commas. An unknown group, a repeated
group or a cap outside 1..65535 is an error: a typo must stop the boot,
not silently run native.
================
*/
func ParseStackSizes(text string) (StackSizes, error) {
	sizes := StackSizes{}
	text = strings.TrimSpace(text)
	if text == "" {
		return sizes, nil
	}
	for _, pair := range strings.Split(text, ",") {
		name, value, found := strings.Cut(strings.TrimSpace(pair), "=")
		name = strings.ToLower(strings.TrimSpace(name))
		if !found || name == "" {
			return nil, fmt.Errorf("%s: %q is not group=cap", EnvStackSizes, pair)
		}
		if _, ok := stackGroupNamed(name); !ok {
			return nil, fmt.Errorf("%s: unknown group %q (known: %s)", EnvStackSizes, name, stackGroupNames())
		}
		if _, repeated := sizes[name]; repeated {
			return nil, fmt.Errorf("%s: group %q given twice", EnvStackSizes, name)
		}
		size, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil || size < 1 || size > maxStackSize {
			return nil, fmt.Errorf("%s: cap %q for %q is not 1..%d", EnvStackSizes, value, name, maxStackSize)
		}
		sizes[name] = uint16(size)
	}
	return sizes, nil
}

/*
================
StackSizesFromEnv
================
*/
func StackSizesFromEnv() (StackSizes, error) {
	return ParseStackSizes(os.Getenv(EnvStackSizes))
}

/*
================
String

The canonical form, sorted by group, for the boot log.
================
*/
func (s StackSizes) String() string {
	pairs := make([]string, 0, len(s))
	for name, size := range s {
		pairs = append(pairs, name+"="+strconv.Itoa(int(size)))
	}
	sort.Strings(pairs)
	return strings.Join(pairs, ",")
}

/*
================
stackGroupNamed
================
*/
func stackGroupNamed(name string) (stackGroup, bool) {
	for _, group := range stackGroups {
		if group.name == name {
			return group, true
		}
	}
	return stackGroup{}, false
}

/*
================
stackGroupNames
================
*/
func stackGroupNames() string {
	names := make([]string, 0, len(stackGroups))
	for _, group := range stackGroups {
		names = append(names, group.name)
	}
	return strings.Join(names, ", ")
}

/*
================
stackGroupOf

The group a row belongs to, or false.
================
*/
func stackGroupOf(ref *ItemRef) (stackGroup, bool) {
	if ref.TypeIDs[0] != 3 || ref.TypeIDs[1] != 3 {
		return stackGroup{}, false
	}
	for _, group := range stackGroups {
		if ref.TypeIDs[2] != group.typeID3 {
			continue
		}
		for _, typeID4 := range group.typeID4s {
			if ref.TypeIDs[3] == typeID4 {
				return group, true
			}
		}
	}
	return stackGroup{}, false
}

/*
================
Raise

Raises one row to its group's override. The native cap moves to
NativeMaxStack so the rules that mean "the original's full stack" (the
beta potion refill) keep the native amount. Returns whether it changed.
================
*/
func (s StackSizes) Raise(ref *ItemRef) bool {
	group, ok := stackGroupOf(ref)
	if !ok {
		return false
	}
	size, ok := s[group.name]
	if !ok {
		return false
	}
	native := ref.NativeFields.Get("maxStack")
	if native < 1 {
		native = 1
	}
	if native >= float64(size) || (native <= 1 && !group.raisesSingles) {
		return false
	}
	ref.NativeMaxStack = uint16(native)
	ref.NativeFields = ref.NativeFields.With("maxStack", float64(size))
	return true
}

/*
================
ApplyStackSizes

Raises the loaded rows once, before UseBoundedCache archives them and
before any gameplay owner reads a cap. Returns how many rows changed.
================
*/
func (t *TextdataItems) ApplyStackSizes(sizes StackSizes) (int, error) {
	t.once.Do(t.load)
	if t.archive != nil {
		return 0, fmt.Errorf("%s: item references are already archived", EnvStackSizes)
	}
	if len(sizes) == 0 {
		return 0, nil
	}
	raised := 0
	for _, ref := range t.byCodename {
		if ref != nil && sizes.Raise(ref) {
			raised++
		}
	}
	return raised, nil
}

/*
================
NativeStackCap

The itemdata MaxStack before any SRO_STACK_SIZES raise.
================
*/
func (r *ItemRef) NativeStackCap() int64 {
	if r.NativeMaxStack != 0 {
		return int64(r.NativeMaxStack)
	}
	return int64(r.NativeFields.Get("maxStack"))
}
