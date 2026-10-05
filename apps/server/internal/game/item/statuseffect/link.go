package statuseffect

import (
	"strconv"
	"strings"
)

// Link is the shared lnks relationship (native execution context+68). Source and target
// effects have distinct wire tokens; the source token identifies this link.
// The registry commits both halves or neither. All returned values are copies.
type Link struct {
	DivisionID, SourceName, TargetName                                  string
	SourceGID, TargetGID                                                uint32
	SourceToken, TargetToken                                            uint32
	SkillID, SkillGroup, Group, MaxDistance, MaxOutgoing, ThreatPercent uint32
	// ManaPercent and ManaCap are lkdh's MP share of the recipient's dealt
	// damage and its per-hit ceiling (Mana Switch); zero means no share.
	ManaHPPercent, ManaPercent, ManaCap uint32
	ExpiresAtMs, StartedAtMs            int64
	ClientCancelable                    bool
	// TargetModifiers are the recipient half's parameter writes (594AC0 in
	// mode 2: stri/inti). 594F53 skips them for the source half, so a link
	// never carries source modifiers.
	TargetModifiers              Modifiers
	sourceRetired, targetRetired bool
}

func linkKey(division string, token uint32) string {
	return strings.ToLower(division) + "\x00" + strconv.FormatUint(uint64(token), 10)
}

// ApplyLink admits the ordinary lnks/lkag shape, without the native +374,
// +49c or +4a0 variants. 59DC80: same target or mixed skill IDs in a nonzero
// link group => 300c; reaching lnks[2] => 3029. Zero means no count limit.
func (r *Registry) ApplyLink(link Link) uint16 {
	if r == nil {
		return 0x3011
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if code := r.linkRefusalLocked(link); code != 0 {
		return code
	}
	source, target := ownerKey(link.DivisionID, link.SourceName), ownerKey(link.DivisionID, link.TargetName)
	link.sourceRetired, link.targetRetired = false, false
	e := Effect{DivisionID: link.DivisionID, SkillID: link.SkillID, SkillGroup: link.SkillGroup,
		LinkToken: link.SourceToken, State: StateActive, ExpiresAtMs: link.ExpiresAtMs, StartedAtMs: link.StartedAtMs,
		DurationPresent: link.ExpiresAtMs != 0, ClientCancelable: link.ClientCancelable}
	recipient := e
	recipient.CharacterName, recipient.OwnerGID, recipient.InstanceToken, recipient.Phase = link.TargetName, link.TargetGID, link.TargetToken, 2
	recipient.Modifiers = link.TargetModifiers
	if !r.bindModifiersLocked(&recipient) {
		return 0x3011
	}
	e.CharacterName, e.OwnerGID, e.InstanceToken, e.Phase = link.SourceName, link.SourceGID, link.SourceToken, 1
	r.byOwner[source] = append(r.byOwner[source], e)
	r.byOwner[target] = append(r.byOwner[target], recipient)
	link.TargetModifiers = Modifiers{}
	r.links[linkKey(link.DivisionID, link.SourceToken)] = link
	// 594EAC installs the latest target effect in ParamKeeper+210. It is
	// a single pointer, not a sum of all incoming links.
	r.threatOwners[target] = link.SourceToken
	return 0
}

// LinkRefusal is ApplyLink's admission alone, so a caster is charged only
// for a link the registry will accept.
func (r *Registry) LinkRefusal(link Link) uint16 {
	if r == nil {
		return 0x3011
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.linkRefusalLocked(link)
}

func (r *Registry) linkRefusalLocked(link Link) uint16 {
	if link.DivisionID == "" || link.SourceName == "" || link.TargetName == "" ||
		strings.EqualFold(link.SourceName, link.TargetName) || link.SourceGID == 0 || link.TargetGID == 0 ||
		link.SourceGID == link.TargetGID || link.SourceToken == 0 || link.TargetToken == 0 ||
		link.SourceToken == link.TargetToken || link.SkillID == 0 || link.SkillGroup == 0 || link.ExpiresAtMs <= 0 {
		return 0x3011
	}
	source, target := ownerKey(link.DivisionID, link.SourceName), ownerKey(link.DivisionID, link.TargetName)
	if len(r.byOwner[source]) >= MaxAttachedEffectsPerCharacter || len(r.byOwner[target]) >= MaxAttachedEffectsPerCharacter {
		return 0x3029
	}
	count := uint32(0)
	for _, old := range r.links {
		if !strings.EqualFold(old.DivisionID, link.DivisionID) || old.sourceRetired || !strings.EqualFold(old.SourceName, link.SourceName) || old.Group != link.Group {
			continue
		}
		if old.SkillID == link.SkillID {
			if old.TargetGID == link.TargetGID {
				return 0x300c
			}
			count++
		} else if link.Group != 0 {
			return 0x300c
		}
	}
	if link.MaxOutgoing != 0 && count >= link.MaxOutgoing {
		return 0x3029
	}
	for _, rows := range r.byOwner {
		for _, old := range rows {
			if strings.EqualFold(old.DivisionID, link.DivisionID) && (old.InstanceToken == link.SourceToken || old.InstanceToken == link.TargetToken) {
				return 0x300c
			}
		}
	}
	return 0
}

// Range/source-loss updates request the source's retirement. The source's
// 5829D0 callback, rather than the request, forces its recipient to retire.
func (r *Registry) stopLinkLocked(division string, token uint32) {
	l, ok := r.links[linkKey(division, token)]
	if !ok {
		return
	}
	if !l.sourceRetired {
		r.stopLinkHalfLocked(l, true)
	} else if !l.targetRetired {
		r.stopLinkHalfLocked(l, false)
	}
}

func (r *Registry) stopLinkHalfLocked(l Link, source bool) {
	name, token := l.TargetName, l.TargetToken
	if source {
		name, token = l.SourceName, l.SourceToken
	}
	key := ownerKey(l.DivisionID, name)
	for i, e := range r.byOwner[key] {
		// Native FindActiveBuffBySkillID accepts pending/active instances and does
		// not skip an already requested stop. A paired recipient uses its exact ID.
		if e.SkillID != l.SkillID || e.InstanceToken != token ||
			(e.State != StatePending && e.State != StateActive) {
			continue
		}
		r.requestReplacementStopLocked(key, i)
		return
	}
}

func (r *Registry) StopLink(division string, token uint32) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stopLinkLocked(division, token)
}

func (r *Registry) retireLinkHalfLocked(e Effect) {
	key := linkKey(e.DivisionID, e.LinkToken)
	l, ok := r.links[key]
	if !ok {
		return
	}
	if e.Phase == 1 {
		l.sourceRetired = true
		if !l.targetRetired {
			r.stopLinkHalfLocked(l, false)
		}
	} else {
		l.targetRetired = true
		// 582A0A clears link+10; the source observes the missing recipient in
		// its subsequent 58484D update, not in the cancellation request.
		l.TargetGID = 0
		// 582C19 clears +210 unconditionally; an older effect retiring does
		// not restore a previous contributor or preserve a newer pointer.
		delete(r.threatOwners, ownerKey(e.DivisionID, e.CharacterName))
	}
	if l.sourceRetired && l.targetRetired {
		delete(r.links, key)
	} else {
		r.links[key] = l
	}
}

func (r *Registry) Links() []Link {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Link, 0, len(r.links))
	for _, l := range r.links {
		out = append(out, l)
	}
	return out
}

// ThreatLink is the installed, logically active target effect. A stop disables
// contribution immediately, before the effect update emits B6A0 retirement.
func (r *Registry) ThreatLink(division, target string, nowMs int64) (Link, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := ownerKey(division, target)
	token := r.threatOwners[key]
	l, ok := r.links[linkKey(division, token)]
	if !ok || l.sourceRetired || l.targetRetired || (Effect{ExpiresAtMs: l.ExpiresAtMs}).Expired(nowMs) {
		return Link{}, false
	}
	for _, e := range r.byOwner[key] {
		if e.LinkToken == token && e.Phase == 2 && !e.StopRequested {
			return l, true
		}
	}
	return Link{}, false
}

/*
==================
ManaLinks

The logically active links whose recipient is target and that hand the
source a share of target's dealt damage (lkdh), and held, the number of
links target receives of any kind. CSkillManager_DistributeSharedDamage
(5A04A0) divides the damage by the size of that list (+0x2E8) before it
pays each lkdh link its share. Unlike the single +210 threat pointer,
every lkdh link counts: each source owns its own share. A stop disables
the share at once, as for ThreatLink. Inferred: a stopped half has left
the native list, so it is not counted.
==================
*/
func (r *Registry) ManaLinks(division, target string, nowMs int64) (links []Link, held int) {
	if r == nil {
		return nil, 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	key := ownerKey(division, target)
	var out []Link
	for _, e := range r.byOwner[key] {
		if e.LinkToken == 0 || e.Phase != 2 || e.StopRequested {
			continue
		}
		held++
		l, ok := r.links[linkKey(division, e.LinkToken)]
		if !ok || l.ManaPercent == 0 && l.ManaHPPercent == 0 || l.sourceRetired || l.targetRetired || (Effect{ExpiresAtMs: l.ExpiresAtMs}).Expired(nowMs) {
			continue
		}
		l.TargetModifiers = Modifiers{}
		out = append(out, l)
	}
	return out, held
}
