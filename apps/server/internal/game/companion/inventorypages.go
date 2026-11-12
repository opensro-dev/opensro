/*
===========================================================================

inventorypages.go - the pet bag page rule, one setting per pet kind

Port-only, not native. The owner approved a five-page pet bag on by
default (2026-11-07, #2770); a grab pet natively holds 28 slots and a
transport its authored size. Grab pets and transports are set apart:
SRO_GRAB_PET_INVENTORY_PAGES sizes a grab pet's bag and
SRO_TRANSPORT_PET_INVENTORY_PAGES a transport's cargo bag. Each takes
"on" (five pages), "off" (native) or a page count from 1 to 9.

The older SRO_PET_INVENTORY_PAGES set both kinds at once. It stays a
fallback for a kind whose own setting is unset, so a shard that turned it
off keeps its native bags: a raised bag is persisted and never lowered,
so a silent fall back to the default would migrate every saved bag.

A page is the native one-page size (28 slots); the client pages a bag of
any capacity generically. Nine pages (252) is the most the one-byte
capacity of 0x3158 can carry.

===========================================================================
*/
package companion

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// EnvPetInventoryPages is the older setting for both pet kinds at once:
// unset or an on value is five pages, an off value is native. Each kind's
// own setting overrides it.
const EnvPetInventoryPages = "SRO_PET_INVENTORY_PAGES"

// EnvGrabPetInventoryPages sizes a grab (pickup) pet's bag.
const EnvGrabPetInventoryPages = "SRO_GRAB_PET_INVENTORY_PAGES"

// EnvTransportPetInventoryPages sizes a transport's cargo bag.
const EnvTransportPetInventoryPages = "SRO_TRANSPORT_PET_INVENTORY_PAGES"

const (
	// PetInventoryPageSlots is one page of a pet bag: the native grab
	// pet bag's size and the client's page.
	PetInventoryPageSlots uint8 = 28
	// DefaultPetInventoryPages is the owner's default and the "on" value.
	DefaultPetInventoryPages uint8 = 5
	// MaxPetInventoryPages keeps the capacity inside one byte (9 x 28 = 252).
	MaxPetInventoryPages uint8 = 9
	// PetInventorySlots is the bag a pet carries at the default five pages.
	PetInventorySlots = DefaultPetInventoryPages * PetInventoryPageSlots
)

/*
================
PetInventoryPages

The pages each pet kind's bag is raised to. Zero is native: the bag
keeps the size it was created with.
================
*/
type PetInventoryPages struct {
	Grab      uint8
	Transport uint8
}

/*
================
PetInventoryPagesFromEnv

Reads both kinds' settings. A kind's own setting wins; unset, it takes
the older shared setting, and with neither set it is the owner's five
pages. Anything unreadable is a boot error naming the setting, the
grammar of the other default-on shard switches: a typo must fail loud,
because a raised bag is persisted and never lowered.
================
*/
func PetInventoryPagesFromEnv() (PetInventoryPages, error) {
	shared, err := sharedPetInventoryPages()
	if err != nil {
		return PetInventoryPages{}, err
	}
	grab, err := petInventoryPagesSetting(EnvGrabPetInventoryPages, shared)
	if err != nil {
		return PetInventoryPages{}, err
	}
	transport, err := petInventoryPagesSetting(EnvTransportPetInventoryPages, shared)
	if err != nil {
		return PetInventoryPages{}, err
	}
	return PetInventoryPages{Grab: grab, Transport: transport}, nil
}

/*
================
sharedPetInventoryPages

The older shared switch: unset or on is five pages, off is native.
================
*/
func sharedPetInventoryPages() (uint8, error) {
	raw := os.Getenv(EnvPetInventoryPages)
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "1", "on", "true":
		return DefaultPetInventoryPages, nil
	case "0", "off", "false":
		return 0, nil
	}
	return 0, fmt.Errorf("%s=%q: want on or off", EnvPetInventoryPages, raw)
}

/*
================
petInventoryPagesSetting

One kind's setting: unset takes the shared value, "on" is five pages,
"off" (or 0) is native, and a number is that many pages.
================
*/
func petInventoryPagesSetting(env string, shared uint8) (uint8, error) {
	raw := os.Getenv(env)
	value := strings.ToLower(strings.TrimSpace(raw))
	switch value {
	case "":
		return shared, nil
	case "on", "true":
		return DefaultPetInventoryPages, nil
	case "off", "false", "0":
		return 0, nil
	}
	pages, err := strconv.Atoi(value)
	if err != nil || pages < 1 || pages > int(MaxPetInventoryPages) {
		return 0, fmt.Errorf("%s=%q: want on, off or a page count from 1 to %d", env, raw, MaxPetInventoryPages)
	}
	return uint8(pages), nil
}

/*
================
GrabCapacity

The capacity a grab pet's bag is created or re-summoned with.
================
*/
func (p PetInventoryPages) GrabCapacity(native uint8) uint8 {
	return raisedPetCapacity(p.Grab, native)
}

/*
================
TransportCapacity

The capacity a transport's cargo bag is created or restored with.
================
*/
func (p PetInventoryPages) TransportCapacity(native uint8) uint8 {
	return raisedPetCapacity(p.Transport, native)
}

/*
================
raisedPetCapacity

Never smaller than the native size, so a bag is only ever raised and its
rows always survive a lower setting.
================
*/
func raisedPetCapacity(pages, native uint8) uint8 {
	slots := min(pages, MaxPetInventoryPages) * PetInventoryPageSlots
	if native < slots {
		return slots
	}
	return native
}
