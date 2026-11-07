/*
===========================================================================

setpublication.go - the equipment set tiers the client's tooltip reads

Port-only, not native: the v1.150 client has no set rows, so a set bonus
is invisible on an item unless the world-entry blob carries the tiers.
The blob names each set group once with its tiers; an item says which
group it belongs to through its own setId native field, and the client
counts the worn pieces itself. The list is empty (and not published)
unless SRO_CAP110_SET_BONUSES is on, so the native blob is unchanged.

===========================================================================
*/

package enterworld

import "sort"

/*
================
SetOptionPublication

One option of a tier, as the client reads it: the magicoption param id
(SetOptionReinforceItem or SetOptionResistAll) and its value.
================
*/
type SetOptionPublication struct {
	Option int `json:"option"`
	Value  int `json:"value"`
}

/*
================
SetTierPublication

One tier of a published set group: the worn-piece count it needs and the
options it applies.
================
*/
type SetTierPublication struct {
	Pieces  int                    `json:"pieces"`
	Options []SetOptionPublication `json:"options"`
}

/*
================
SetPublication

One set group: its id (an item's setId), the name symbol the overlay
authored, and its tiers ascending by piece count.
================
*/
type SetPublication struct {
	ID    int64                `json:"id"`
	Name  string               `json:"name,omitempty"`
	Tiers []SetTierPublication `json:"tiers"`
}

/*
================
SetPublisher

The optional capability of a set catalogue that can list its groups.
================
*/
type SetPublisher interface {
	PublishedSets() []SetPublication
}

/*
================
PublishedSets

Every loaded set group with its tiers, ascending by id.
================
*/
func (t *TextdataSetGroups) PublishedSets() []SetPublication {
	t.once.Do(t.load)
	ids := make([]int64, 0, len(t.tiers))
	for id := range t.tiers {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	out := make([]SetPublication, 0, len(ids))
	for _, id := range ids {
		set := SetPublication{ID: id, Name: t.names[id]}
		for _, tier := range t.tiers[id] {
			published := SetTierPublication{Pieces: tier.RequiredCount, Options: []SetOptionPublication{}}
			for _, option := range t.options[tier.MagicGroupID] {
				published.Options = append(published.Options, SetOptionPublication{Option: option.OptionID, Value: option.Value})
			}
			set.Tiers = append(set.Tiers, published)
		}
		out = append(out, set)
	}
	return out
}

/*
================
PublishedSets

The sets the world-entry blob carries: none unless the set-bonus flag is
on, so the native blob never names a set.
================
*/
func (d *Deps) PublishedSets() []SetPublication {
	publisher, ok := d.SetCatalogue().(SetPublisher)
	if !ok {
		return nil
	}
	return publisher.PublishedSets()
}
