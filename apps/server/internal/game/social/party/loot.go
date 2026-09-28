/*
===========================================================================

loot.go - party-owned rotating pickup recipients

Retail 4D4840 moves the first member entry to the tail and returns its GID.
Pickup eligibility is evaluated by the action owner after this rotation.

===========================================================================
*/
package party

/*
================
NextLootMember

Reconcile the rotation against current membership without changing roster
order. The same registry lock owns joins, leaves and the pickup cursor.
================
*/
func (r *Registry) NextLootMember(division, name string) uint32 {
	r.mu.Lock()
	defer r.mu.Unlock()
	p := r.byKey[memberKey(division, name)]
	if p == nil || p.optionBits&PartyOptionItemShare == 0 {
		return 0
	}
	members := make(map[uint32]bool, len(p.members))
	for _, member := range p.members {
		members[member.MemberID] = true
	}
	order := p.lootOrder[:0]
	for _, gid := range p.lootOrder {
		if members[gid] {
			order = append(order, gid)
			delete(members, gid)
		}
	}
	for _, member := range p.members {
		if members[member.MemberID] {
			order = append(order, member.MemberID)
		}
	}
	if len(order) == 0 {
		p.lootOrder = nil
		return 0
	}
	gid := order[0]
	copy(order, order[1:])
	order[len(order)-1] = gid
	p.lootOrder = order
	return gid
}
