/*
===========================================================================

catalog.go - the alchemy reference catalog

Package alchemy owns reference-driven Alchemy rules. It never mutates a
character or writes to a session; action commits its detached plans. The
catalog is the itemdata, magicoption and magicoptionassign projection
those rules read.

===========================================================================
*/

package alchemy

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/inventory"
)

/*
================
Reference

One itemdata row as alchemy reads it.
================
*/
type Reference struct {
	ID           uint32
	Name         string
	Flags        uint16
	Class        int
	Rarity       int
	MaxMagic     int
	Price        uint32
	Stack        uint16
	Params       [5]uint32
	Descriptions [5]string
}

/*
================
Magic

One magicoption.txt row: param id, codename, degree, tag, the three
generator params and the item categories it may land on.
================
*/
type Magic struct {
	ID         uint16
	Name       string
	Degree     int
	Tag        uint32
	Params     [3]uint32
	Categories []string
}

/*
================
Catalog
================
*/
type Catalog struct {
	Items map[string]Reference
	Magic map[uint16]Magic
	// Loaded through the explicit v1.150 compatibility profile. Missing or
	// malformed pools still refuse before mutation; no on-demand fallback.
	DissolveDrops map[int]DissolvePool
	// AvatarOptions are the option codenames magicoptionassign.txt gives
	// each avatar part, keyed by TID4 (avatar.go).
	AvatarOptions map[uint8][]string
}

/*
================
LoadCatalog
================
*/
func LoadCatalog(dir string, source enterworld.ItemRefSource) (*Catalog, error) {
	c := &Catalog{Items: map[string]Reference{}, Magic: map[uint16]Magic{}}
	files, err := filepath.Glob(filepath.Join(dir, "itemdata*.txt"))
	if err != nil {
		return nil, err
	}
	for _, path := range files {
		for _, a := range enterworld.ReadTextdataFile(path) {
			if len(a) < 128 || a[0] != "1" {
				continue
			}
			ref, ok := source.ItemRefByCodename(a[2])
			if !ok || ref == nil {
				return nil, fmt.Errorf("alchemy: missing item reference %s", a[2])
			}
			r := Reference{ID: ref.RefObjID, Name: a[2], Flags: ref.TypeFlags()}
			price, pe := strconv.ParseInt(a[26], 10, 64)
			stack, se := strconv.ParseInt(a[57], 10, 64)
			if pe != nil || se != nil || price < -1 || price > 0x7fffffff || stack < -1 || stack > 65535 {
				return nil, fmt.Errorf("alchemy: invalid price/stack %s", a[2])
			}
			r.Price, r.Stack = uint32(max(price, 0)), uint16(max(stack, 0))
			if len(a) < 159 {
				return nil, fmt.Errorf("alchemy: incomplete item %s", a[2])
			}
			r.Rarity, err = strconv.Atoi(a[15])
			if err != nil {
				return nil, err
			}
			r.MaxMagic, err = strconv.Atoi(a[158])
			if err != nil || r.MaxMagic < 0 || r.MaxMagic > 12 {
				return nil, fmt.Errorf("alchemy: invalid magic capacity %s", a[2])
			}
			r.Class, err = strconv.Atoi(a[61])
			if err != nil {
				return nil, fmt.Errorf("alchemy: %s item class: %w", a[2], err)
			}
			for i := range r.Params {
				n, e := strconv.ParseInt(a[118+2*i], 10, 64)
				if e != nil || n < -1 || n > 0xffffffff {
					return nil, fmt.Errorf("alchemy: %s parameter %d", a[2], i+1)
				}
				r.Params[i] = uint32(n)
				r.Descriptions[i] = a[119+2*i]
			}
			c.Items[r.Name] = r
		}
	}
	for _, a := range enterworld.ReadTextdataFile(filepath.Join(dir, "magicoption.txt")) {
		if len(a) < 31 || a[0] != "1" {
			continue
		}
		id, e := strconv.ParseUint(a[1], 10, 16)
		if e != nil || id == 0 {
			return nil, fmt.Errorf("alchemy: invalid magic id")
		}
		degree, e := strconv.Atoi(a[4])
		if e != nil {
			return nil, e
		}
		tag, e := strconv.ParseUint(a[7], 10, 32)
		if e != nil {
			return nil, e
		}
		m := Magic{ID: uint16(id), Name: a[2], Degree: degree, Tag: uint32(tag)}
		for i := range m.Params {
			n, e := strconv.ParseUint(a[8+i], 10, 32)
			if e != nil {
				return nil, e
			}
			m.Params[i] = uint32(n)
		}
		for i := 29; i+1 < len(a); i += 2 {
			if a[i] != "xxx" && a[i+1] != "0" {
				m.Categories = append(m.Categories, strings.ToLower(a[i]))
			}
		}
		if _, exists := c.Magic[m.ID]; exists {
			return nil, fmt.Errorf("alchemy: duplicate magic id %d", m.ID)
		}
		c.Magic[m.ID] = m
	}
	if len(c.Items) == 0 || len(c.Magic) == 0 {
		return nil, fmt.Errorf("alchemy: itemdata and magicoption tables are required")
	}
	if _, ok := c.Option("MATTR_DEC_MAXDUR", 3); !ok {
		return nil, fmt.Errorf("alchemy: missing native durability curse level 3")
	}
	if err := c.loadAvatarOptions(dir); err != nil {
		return nil, err
	}
	if err := c.loadDissolveProfile(); err != nil {
		return nil, err
	}
	return c, nil
}

/*
================
Option

The magicoption row with this codename at this degree.
================
*/
func (c *Catalog) Option(name string, degree int) (Magic, bool) {
	for _, m := range c.Magic {
		if m.Name == name && m.Degree == degree {
			return m, true
		}
	}
	return Magic{}, false
}

/*
================
Reference.Degree

The item's degree: equipment classes come in threes per degree.
================
*/
func (r Reference) Degree() int {
	if r.Class <= 0 {
		return 0
	}
	if r.Flags&0x7c == 0x2c {
		return (r.Class-1)/3 + 1
	}
	return r.Class
}

/*
================
category
================
*/
func category(flags uint16) string {
	if flags&0x7e != 0x2c {
		return ""
	}
	switch (flags >> 7) & 15 {
	case 6:
		return "weapon"
	case 4:
		return "shield"
	case 5, 12:
		return "accessory"
	case 1, 2, 3, 9, 10, 11:
		return "armor"
	}
	return ""
}

/*
================
Magic.Allows
================
*/
func (m Magic) Allows(flags uint16) bool {
	itemCategory := category(flags)
	// magicoption.txt can constrain an option to a body part (HP/MP use
	// helm/mail/pants), not just the broad armor family. Resolve those names
	// through the shared equipment socket owner; treating them as "armor"
	// would incorrectly admit shoulders, gloves and boots.
	bodyPart := ""
	if itemCategory == "armor" {
		socket, valid := inventory.EquipSocketForTypeFlags(flags)
		if valid {
			switch socket {
			case inventory.SocketHead:
				bodyPart = "helm"
			case inventory.SocketBody:
				bodyPart = "mail"
			case inventory.SocketLeg:
				bodyPart = "pants"
			}
		}
	}
	for _, c := range m.Categories {
		if c != "" && (c == itemCategory || c == bodyPart) {
			return true
		}
	}
	return false
}
