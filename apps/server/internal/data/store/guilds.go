package store

// The guild plane's door (domain.GuildStore over the guilds and
// guild_members tables): the memos-door pattern with a two-part dirty
// unit - one guildKey marks the guild row AND its whole member set, and
// the commit rewrites both with a whole-set replace (replaceGuildTx).

import (
	"fmt"
	"math"
	"strings"

	"opensro.online/server/internal/domain"
)

// Guilds returns the guild door (domain.GuildStore over the guilds /
// guild_members tables). Same lifetime contract as Characters().
func (s *Store) Guilds() domain.GuildStore {
	return storeGuildDoor{s: s}
}

type storeGuildDoor struct{ s *Store }

// Guild returns copies of the guild row and its member list in wire order;
// ok=false when no such guild is stored.
func (door storeGuildDoor) Guild(divisionID string, guildID int64) (domain.GuildRecord, []domain.GuildMemberRecord, bool) {
	door.s.mu.RLock()
	defer door.s.mu.RUnlock()
	guild, ok := door.s.guilds[divisionID][guildID]
	if !ok {
		return domain.GuildRecord{}, nil, false
	}
	members, coherent := door.guildMemberViewsLocked(
		divisionID,
		door.s.guildMembers[divisionID][guildID],
	)
	if !coherent {
		return domain.GuildRecord{}, nil, false
	}
	return guild, members, true
}

// GuildOfCharacter answers the guild a character is a MEMBER of, resolved
// from the stored member sets (the honest source: the character record's
// GuildID FK points here, never the other way around).
func (door storeGuildDoor) GuildOfCharacter(divisionID string, characterID int64) (int64, bool) {
	door.s.mu.RLock()
	defer door.s.mu.RUnlock()
	for guildID, members := range door.s.guildMembers[divisionID] {
		for _, member := range members {
			if member.CharID == characterID {
				return guildID, true
			}
		}
	}
	return 0, false
}

// UpdateGuildAs is the existing-guild metadata/member-field command door.
// Authorization and the update execute under the same store lock; roster
// topology cannot pass through this callback.
func (door storeGuildDoor) UpdateGuildAs(
	divisionID string,
	actorID int64,
	label string,
	authorization domain.GuildAuthorization,
	update func(
		guild domain.GuildRecord,
		members []domain.GuildMemberRecord,
	) (domain.GuildRecord, []domain.GuildMemberRecord, bool),
) (domain.GuildSnapshot, domain.GuildRefusal) {
	var refused domain.GuildSnapshot
	s := door.s
	s.mu.Lock()
	defer s.mu.Unlock()

	guildID, guild, liveMembers, _, _, refusal :=
		door.authorizedGuildActorLocked(divisionID, actorID, authorization)
	if refusal.Refused() {
		return refused, refusal
	}

	if update == nil {
		return refused, domain.GuildRefusalUpdateRejected
	}
	snapshot := make([]domain.GuildMemberRecord, len(liveMembers))
	copy(snapshot, liveMembers)
	nextGuild, nextMembers, changed := update(guild, snapshot)
	if !changed || !door.validGuildUpdateLocked(
		divisionID,
		guildID,
		liveMembers,
		nextGuild,
		nextMembers,
	) {
		return refused, domain.GuildRefusalUpdateRejected
	}

	storedMembers := guildMembersForStorage(nextMembers)
	s.guilds[divisionID][guildID] = nextGuild
	s.guildMembers[divisionID][guildID] = storedMembers
	s.changes.guilds[guildKey{division: divisionID, guildID: guildID}] = true
	s.commitLocked(label)

	committedMembers, coherent := door.guildMemberViewsLocked(divisionID, storedMembers)
	if !coherent {
		return refused, domain.GuildRefusalInconsistentMembership
	}
	return domain.GuildSnapshot{
		Guild:   nextGuild,
		Members: committedMembers,
	}, domain.GuildRefusalNone
}

// CreateGuild is the ATOMIC guild-creation door: the guild row, the
// leader's member row, the leader character's GuildID FK and the guild-id
// watermark all move under ONE lock hold and commit in ONE transaction.
// Two sequential guild/character doors would not be
// atomic - a crash between the commits tears guild-vs-FK state, which is
// exactly the torn state the FK's dangling-link guard logs loud about.
//
// The guild id allocates from the per-division watermark (next_char_id's
// twin: allocate, increment, never reuse), falling back to
// max(live guild id)+1 for a division whose watermark was never seeded
// (a fresh empty environment). The returned id is also written into the
// installed guild record and the leader character's FK.
//
// The error is for VALIDATION only (the CreateCharacter posture): a
// case-insensitive name conflict among the division's guilds refuses
// before any mutation - EqualFold, the same uniqueness rule
// CreateCharacter applies to character names (a DECISION documented in
// internal/game/social/guild; no native pin exists for guild-name collation). Write
// failures follow the door's fail-open-loud rule like every other commit.
func (door storeGuildDoor) CreateGuild(divisionID string, guild domain.GuildRecord, leader domain.GuildMemberRecord, leaderCharacter *domain.Character) (int64, error) {
	s := door.s
	if leaderCharacter == nil {
		return 0, fmt.Errorf("nil leader character")
	}
	if strings.TrimSpace(guild.Name) == "" {
		return 0, fmt.Errorf("guild name empty")
	}
	if len(guild.Name) > domain.GuildNameMaxBytes ||
		len(guild.NoticeSubject) > domain.GuildNoticeSubjectBytes ||
		len(guild.NoticeContents) > domain.GuildNoticeBodyBytes {
		return 0, fmt.Errorf("guild record exceeds native string bounds")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if actualDivision, known := s.charDivision[leaderCharacter]; !known || actualDivision != divisionID {
		return 0, fmt.Errorf("leader character %q is not live in division %s", leaderCharacter.Name, divisionID)
	}
	if leader.CharID != leaderCharacter.ID {
		return 0, fmt.Errorf("leader member id %d does not match character id %d", leader.CharID, leaderCharacter.ID)
	}
	if leader.Name != leaderCharacter.Name {
		return 0, fmt.Errorf("leader member name %q does not match character name %q", leader.Name, leaderCharacter.Name)
	}
	if leader.Grade != 0 {
		return 0, fmt.Errorf("leader member grade %d must be 0", leader.Grade)
	}
	if leaderCharacter.DeletePending {
		return 0, fmt.Errorf("leader character %q is pending deletion", leaderCharacter.Name)
	}
	if leaderCharacter.GuildID != nil {
		return 0, fmt.Errorf("leader character %q already belongs to guild %d", leaderCharacter.Name, *leaderCharacter.GuildID)
	}
	if existingGuildID, joined := door.guildOfCharacterLocked(divisionID, leaderCharacter.ID); joined {
		return 0, fmt.Errorf("leader character %q already has a member row in guild %d", leaderCharacter.Name, existingGuildID)
	}
	for _, existing := range s.guilds[divisionID] {
		if strings.EqualFold(existing.Name, guild.Name) {
			return 0, fmt.Errorf("guild %q already exists in division %s", guild.Name, divisionID)
		}
	}
	id := s.meta.NextGuildID[divisionID]
	if id < 1 {
		id = s.maxGuildIDLocked(divisionID) + 1
	}
	if id < 1 || id > domain.MaxGuildID {
		return 0, fmt.Errorf("guild id space exhausted for division %s (next %d, maximum %d)", divisionID, id, domain.MaxGuildID)
	}
	guild.ID = id
	if err := validateGuildRecord(guild); err != nil {
		return 0, err
	}
	if len(leader.GrantName) > domain.GuildGrantNameMaxBytes {
		return 0, fmt.Errorf("leader grant name exceeds %d bytes", domain.GuildGrantNameMaxBytes)
	}
	if s.guilds[divisionID] == nil {
		s.guilds[divisionID] = map[int64]domain.GuildRecord{}
	}
	if s.guildMembers[divisionID] == nil {
		s.guildMembers[divisionID] = map[int64][]domain.GuildMemberRecord{}
	}
	s.guilds[divisionID][id] = guild
	s.guildMembers[divisionID][id] = guildMembersForStorage(
		[]domain.GuildMemberRecord{leader},
	)
	leaderCharacter.GuildID = &id
	s.meta.NextGuildID[divisionID] = id + 1
	s.changes.guilds[guildKey{division: divisionID, guildID: id}] = true
	s.changes.nextGuildID[divisionID] = true
	s.changes.characters[leaderCharacter] = true
	s.commitLocked("guild-create " + guild.Name)
	return id, nil
}

// KickGuildMember authorizes and applies one exact-name kick under one lock.
func (door storeGuildDoor) KickGuildMember(
	divisionID string,
	actorID int64,
	targetName string,
	requiredPermission uint32,
) (domain.GuildRemovalResult, domain.GuildRefusal) {
	var refused domain.GuildRemovalResult
	s := door.s
	s.mu.Lock()
	defer s.mu.Unlock()

	guildID, guild, members, _, _, refusal :=
		door.authorizedGuildActorLocked(
			divisionID,
			actorID,
			domain.GuildAuthorization{RequiredPermission: requiredPermission},
		)
	if refusal.Refused() {
		return refused, refusal
	}
	targetIndex := -1
	for i := range members {
		if members[i].Name == targetName {
			targetIndex = i
			break
		}
	}
	if targetIndex < 0 {
		return refused, domain.GuildRefusalTargetNotFound
	}
	if members[targetIndex].CharID == actorID {
		return refused, domain.GuildRefusalSelfTarget
	}
	if members[targetIndex].Grade == 0 {
		return refused, domain.GuildRefusalTargetLeader
	}
	if guild.Vote != nil && guild.Vote.Involves(members[targetIndex].JID) {
		return refused, domain.GuildRefusalVoteInProgress
	}
	return door.removeGuildMemberLocked(
		divisionID,
		guildID,
		guild,
		members,
		targetIndex,
		"guild-kick-member",
	)
}

// LeaveGuild removes the acting non-leader under the same authority lock that
// proves their current membership and FK.
func (door storeGuildDoor) LeaveGuild(
	divisionID string,
	actorID int64,
) (domain.GuildRemovalResult, domain.GuildRefusal) {
	var refused domain.GuildRemovalResult
	s := door.s
	s.mu.Lock()
	defer s.mu.Unlock()

	guildID, guild, members, actorIndex, _, refusal :=
		door.authorizedGuildActorLocked(divisionID, actorID, domain.GuildAuthorization{})
	if refusal.Refused() {
		return refused, refusal
	}
	if members[actorIndex].Grade == 0 {
		return refused, domain.GuildRefusalLeaderCannotLeave
	}
	if guild.Vote != nil && guild.Vote.Involves(members[actorIndex].JID) {
		return refused, domain.GuildRefusalVoteInProgress
	}
	return door.removeGuildMemberLocked(
		divisionID,
		guildID,
		guild,
		members,
		actorIndex,
		"guild-leave",
	)
}

// AddGuildMemberAs authorizes the inviter and installs the member row plus
// joining character FK under one lock.
func (door storeGuildDoor) AddGuildMemberAs(
	divisionID string,
	expectedGuildID int64,
	actorID int64,
	requiredPermission uint32,
	member domain.GuildMemberRecord,
) (domain.GuildSnapshot, domain.GuildRefusal) {
	var refused domain.GuildSnapshot
	s := door.s
	s.mu.Lock()
	defer s.mu.Unlock()

	guildID, guild, members, _, _, refusal :=
		door.authorizedGuildActorLocked(
			divisionID,
			actorID,
			domain.GuildAuthorization{RequiredPermission: requiredPermission},
		)
	if refusal.Refused() {
		return refused, refusal
	}
	if guildID != expectedGuildID {
		return refused, domain.GuildRefusalGuildChanged
	}
	if member.Grade == 0 {
		return refused, domain.GuildRefusalInvalidMember
	}
	if len(members) >= min(domain.GuildMemberMaxCount, domain.GuildMemberCapacity(guild.Level)) {
		return refused, domain.GuildRefusalRosterFull
	}
	for _, existing := range members {
		if existing.CharID == member.CharID || existing.JID == member.JID {
			return refused, domain.GuildRefusalAlreadyMember
		}
	}
	joiner := s.characterByIDLocked(divisionID, member.CharID)
	if joiner == nil || joiner.DeletePending || member.Name != joiner.Name ||
		len(member.GrantName) > domain.GuildGrantNameMaxBytes {
		return refused, domain.GuildRefusalInvalidMember
	}
	if joiner.GuildID != nil {
		return refused, domain.GuildRefusalAlreadyMember
	}
	if _, joined := door.guildOfCharacterLocked(divisionID, joiner.ID); joined {
		return refused, domain.GuildRefusalAlreadyMember
	}
	// Copy-then-swap: the fresh slice replaces the live one, so a member
	// list a concurrent Guild() call copied keeps its view.
	next := make([]domain.GuildMemberRecord, len(members), len(members)+1)
	copy(next, members)
	next = append(next, member)
	storedMembers := guildMembersForStorage(next)
	s.guildMembers[divisionID][guildID] = storedMembers
	id := guildID
	joiner.GuildID = &id
	s.changes.guilds[guildKey{division: divisionID, guildID: guildID}] = true
	s.changes.characters[joiner] = true
	s.commitLocked(fmt.Sprintf("guild-add-member %s/%d", divisionID, guildID))

	committedMembers, coherent := door.guildMemberViewsLocked(divisionID, storedMembers)
	if !coherent {
		return refused, domain.GuildRefusalInconsistentMembership
	}
	return domain.GuildSnapshot{
		Guild:   guild,
		Members: committedMembers,
	}, domain.GuildRefusalNone
}

// DissolveGuildAs authorizes the acting leader and dissolves the coherent
// aggregate under one lock. Any FK drift refuses the entire command.
func (door storeGuildDoor) DissolveGuildAs(
	divisionID string,
	actorID int64,
) (domain.GuildSnapshot, domain.GuildRefusal) {
	var refused domain.GuildSnapshot
	s := door.s
	s.mu.Lock()
	defer s.mu.Unlock()

	guildID, guild, members, _, _, refusal :=
		door.authorizedGuildActorLocked(
			divisionID,
			actorID,
			domain.GuildAuthorization{LeaderOnly: true},
		)
	if refusal.Refused() {
		return refused, refusal
	}
	memberCharacters := make([]*domain.Character, 0, len(members))
	for _, member := range members {
		character := s.characterByIDLocked(divisionID, member.CharID)
		if character == nil ||
			character.GuildID == nil ||
			*character.GuildID != guildID {
			return refused, domain.GuildRefusalInconsistentMembership
		}
		memberCharacters = append(memberCharacters, character)
	}
	delete(s.guilds[divisionID], guildID)
	delete(s.guildMembers[divisionID], guildID)
	key := guildKey{division: divisionID, guildID: guildID}
	// A retained dirty mark from an earlier FAILED commit would now
	// point at no live row and fail every commit; the DELETE-only set
	// subsumes it (deleting the rows persists strictly more than
	// re-writing their last pre-dissolve state would have).
	delete(s.changes.guilds, key)
	s.changes.dissolvedGuild[key] = true
	for _, character := range memberCharacters {
		character.GuildID = nil
		s.changes.characters[character] = true
	}
	s.commitLocked(fmt.Sprintf("guild-dissolve %s/%d", divisionID, guildID))

	resultMembers := make([]domain.GuildMemberRecord, len(members))
	copy(resultMembers, members)
	return domain.GuildSnapshot{
		Guild:   guild,
		Members: resultMembers,
	}, domain.GuildRefusalNone
}

// DonateGuildPoints is the ATOMIC guild-point donation door (the 0x740F
// GP-donate job): debit the donating character's SP, credit the guild
// row's GP and the donor's member-row DonatedGP - all under ONE lock
// hold and ONE commit (the CreateGuild rationale: two sequential doors
// would tear SP-vs-GP state on a crash between commits, minting or
// destroying points). The character resolves by id INSIDE the door and
// every precondition re-validates under the lock (the AddGuildMemberAs
// posture - the caller's pre-checks ran without it): guild stored,
// donor's member row present, character record known, amount >= 1,
// amount <= the character's SP (nil or negative SP reads as 0), and
// neither the guild GP nor the member DonatedGP u32 may wrap. Returns
// the new guild GP and the donor's new DonatedGP; a non-zero refusal
// means no mutation and no commit.
func (door storeGuildDoor) DonateGuildPoints(
	divisionID string,
	characterID int64,
	amount uint32,
) (domain.GuildDonationResult, domain.GuildRefusal) {
	var refused domain.GuildDonationResult
	s := door.s
	s.mu.Lock()
	defer s.mu.Unlock()

	guildID, guild, members, memberIndex, donor, refusal :=
		door.authorizedGuildActorLocked(
			divisionID,
			characterID,
			domain.GuildAuthorization{},
		)
	if refusal.Refused() {
		return refused, refusal
	}
	sp := int64(0)
	if donor.SkillPoints != nil && *donor.SkillPoints > 0 {
		sp = *donor.SkillPoints
	}
	if amount < 1 {
		return refused, domain.GuildRefusalInvalidAmount
	}
	if int64(amount) > sp {
		return refused, domain.GuildRefusalInsufficientPoints
	}
	if guild.GP > 0xffffffff-amount || members[memberIndex].DonatedGP > 0xffffffff-amount {
		return refused, domain.GuildRefusalNumericOverflow
	}
	// Copy-then-swap on the member slice (the RemoveGuildMember rule:
	// a list a concurrent Guild() call copied keeps its view).
	next := make([]domain.GuildMemberRecord, len(members))
	copy(next, members)
	next[memberIndex].DonatedGP += amount
	guild.GP += amount
	remaining := sp - int64(amount)
	donor.SkillPoints = &remaining
	s.guilds[divisionID][guildID] = guild
	storedMembers := guildMembersForStorage(next)
	s.guildMembers[divisionID][guildID] = storedMembers
	s.changes.guilds[guildKey{division: divisionID, guildID: guildID}] = true
	s.changes.characters[donor] = true
	s.commitLocked(fmt.Sprintf("guild-gp-donate %s/%d", divisionID, guildID))

	committedMembers, coherent := door.guildMemberViewsLocked(divisionID, storedMembers)
	if !coherent {
		return refused, domain.GuildRefusalInconsistentMembership
	}
	return domain.GuildDonationResult{
		Snapshot: domain.GuildSnapshot{
			Guild:   guild,
			Members: committedMembers,
		},
		Donor: committedMembers[memberIndex],
	}, domain.GuildRefusalNone
}

// LevelUpGuildAs is the ATOMIC level-up door (the guild manager's 0x73F0,
// v1.188 0x70FA -> 5C63D0, guild job 9): the acting
// leader's guild pays the next level's GP, the leader pays its gold, and
// the level rises by one, under ONE lock hold and ONE commit. 5C6240 tests
// the gold (0x4C31) before the GP (0x4C32).
func (door storeGuildDoor) LevelUpGuildAs(divisionID string, actorID int64) (domain.GuildSnapshot, domain.GuildRefusal) {
	var refused domain.GuildSnapshot
	s := door.s
	s.mu.Lock()
	defer s.mu.Unlock()

	guildID, guild, members, _, leader, refusal := door.authorizedGuildActorLocked(
		divisionID, actorID, domain.GuildAuthorization{LeaderOnly: true})
	if refusal.Refused() {
		return refused, refusal
	}
	cost, ok := domain.GuildLevelUpCostAt(guild.Level)
	if !ok {
		return refused, domain.GuildRefusalMaxLevel
	}
	if leader.Gold == nil || *leader.Gold < cost.Gold {
		return refused, domain.GuildRefusalGoldDeficit
	}
	if guild.GP < cost.GP {
		return refused, domain.GuildRefusalGPDeficit
	}
	gold := *leader.Gold - cost.Gold
	leader.Gold = &gold
	guild.GP -= cost.GP
	guild.Level++
	s.guilds[divisionID][guildID] = guild
	s.changes.guilds[guildKey{division: divisionID, guildID: guildID}] = true
	s.changes.characters[leader] = true
	s.commitLocked(fmt.Sprintf("guild-level-up %s/%d", divisionID, guildID))
	return domain.GuildSnapshot{Guild: guild, Members: members}, domain.GuildRefusalNone
}

// ClaimWarCompensationAs is the ATOMIC compensation door (the guild
// manager's 0x73F7, v1.188 0x7113 -> 5C7330, guild job 0x1E): the acting
// leader is paid what guild wars owe the guild and the debt clears, under
// ONE lock hold and ONE commit. Nothing owed refuses (0x4C45).
func (door storeGuildDoor) ClaimWarCompensationAs(divisionID string, actorID int64) (int64, domain.GuildRefusal) {
	s := door.s
	s.mu.Lock()
	defer s.mu.Unlock()

	guildID, guild, _, _, leader, refusal := door.authorizedGuildActorLocked(
		divisionID, actorID, domain.GuildAuthorization{LeaderOnly: true})
	if refusal.Refused() {
		return 0, refusal
	}
	amount := guild.WarCompensation
	if amount <= 0 {
		return 0, domain.GuildRefusalNoCompensation
	}
	gold := int64(0)
	if leader.Gold != nil {
		gold = *leader.Gold
	}
	if gold > math.MaxInt64-amount {
		return 0, domain.GuildRefusalNumericOverflow
	}
	gold += amount
	leader.Gold = &gold
	guild.WarCompensation = 0
	s.guilds[divisionID][guildID] = guild
	s.changes.guilds[guildKey{division: divisionID, guildID: guildID}] = true
	s.changes.characters[leader] = true
	s.commitLocked(fmt.Sprintf("guild-war-compensation %s/%d", divisionID, guildID))
	return amount, domain.GuildRefusalNone
}

func (door storeGuildDoor) guildOfCharacterLocked(
	divisionID string,
	characterID int64,
) (int64, bool) {
	for guildID, members := range door.s.guildMembers[divisionID] {
		for _, member := range members {
			if member.CharID == characterID {
				return guildID, true
			}
		}
	}
	return 0, false
}

func (door storeGuildDoor) authorizedGuildActorLocked(
	divisionID string,
	actorID int64,
	authorization domain.GuildAuthorization,
) (
	guildID int64,
	guild domain.GuildRecord,
	members []domain.GuildMemberRecord,
	actorIndex int,
	actor *domain.Character,
	refusal domain.GuildRefusal,
) {
	actor = door.s.characterByIDLocked(divisionID, actorID)
	if actor == nil {
		return 0, domain.GuildRecord{}, nil, -1, nil, domain.GuildRefusalActorNotFound
	}
	if actor.DeletePending {
		return 0, domain.GuildRecord{}, nil, -1, nil, domain.GuildRefusalDeletePending
	}
	if actor.GuildID == nil {
		return 0, domain.GuildRecord{}, nil, -1, nil, domain.GuildRefusalNotMember
	}
	guildID, joined := door.guildOfCharacterLocked(divisionID, actorID)
	if !joined || *actor.GuildID != guildID {
		return 0, domain.GuildRecord{}, nil, -1, nil, domain.GuildRefusalInconsistentMembership
	}
	guild, exists := door.s.guilds[divisionID][guildID]
	if !exists {
		return 0, domain.GuildRecord{}, nil, -1, nil, domain.GuildRefusalInconsistentMembership
	}
	members, coherent := door.guildMemberViewsLocked(
		divisionID,
		door.s.guildMembers[divisionID][guildID],
	)
	if !coherent {
		return 0, domain.GuildRecord{}, nil, -1, nil, domain.GuildRefusalInconsistentMembership
	}
	actorIndex = -1
	for i := range members {
		if members[i].CharID == actorID {
			actorIndex = i
			break
		}
	}
	if actorIndex < 0 {
		return 0, domain.GuildRecord{}, nil, -1, nil, domain.GuildRefusalInconsistentMembership
	}
	actorMember := members[actorIndex]
	if authorization.RequiredPermission != 0 &&
		actorMember.PermMask&authorization.RequiredPermission == 0 {
		return 0, domain.GuildRecord{}, nil, -1, nil, domain.GuildRefusalPermissionDenied
	}
	if authorization.LeaderOnly && actorMember.Grade != 0 {
		return 0, domain.GuildRecord{}, nil, -1, nil, domain.GuildRefusalLeaderRequired
	}
	return guildID, guild, members, actorIndex, actor, domain.GuildRefusalNone
}

func (door storeGuildDoor) validGuildUpdateLocked(
	divisionID string,
	guildID int64,
	liveMembers []domain.GuildMemberRecord,
	nextGuild domain.GuildRecord,
	nextMembers []domain.GuildMemberRecord,
) bool {
	if validateGuildRecord(nextGuild) != nil ||
		len(nextMembers) != len(liveMembers) {
		return false
	}
	for otherID, other := range door.s.guilds[divisionID] {
		if otherID != guildID && strings.EqualFold(other.Name, nextGuild.Name) {
			return false
		}
	}

	leaders := 0
	seenJIDs := make(map[uint32]bool, len(nextMembers))
	for i, member := range nextMembers {
		if member.CharID != liveMembers[i].CharID {
			return false
		}
		character := door.s.characterByIDLocked(divisionID, member.CharID)
		if character == nil ||
			character.DeletePending ||
			character.GuildID == nil ||
			*character.GuildID != guildID ||
			member.Name != character.Name ||
			len(member.GrantName) > domain.GuildGrantNameMaxBytes ||
			seenJIDs[member.JID] {
			return false
		}
		seenJIDs[member.JID] = true
		if member.Grade == 0 {
			leaders++
		}
	}
	return leaders == 1
}

func (door storeGuildDoor) removeGuildMemberLocked(
	divisionID string,
	guildID int64,
	guild domain.GuildRecord,
	members []domain.GuildMemberRecord,
	targetIndex int,
	label string,
) (domain.GuildRemovalResult, domain.GuildRefusal) {
	var refused domain.GuildRemovalResult
	if targetIndex < 0 || targetIndex >= len(members) {
		return refused, domain.GuildRefusalTargetNotFound
	}
	target := members[targetIndex]
	character := door.s.characterByIDLocked(divisionID, target.CharID)
	if character == nil ||
		character.GuildID == nil ||
		*character.GuildID != guildID {
		return refused, domain.GuildRefusalInconsistentMembership
	}

	membersBefore := make([]domain.GuildMemberRecord, len(members))
	copy(membersBefore, members)
	kept := make([]domain.GuildMemberRecord, 0, len(members)-1)
	kept = append(kept, members[:targetIndex]...)
	kept = append(kept, members[targetIndex+1:]...)
	door.s.guildMembers[divisionID][guildID] = guildMembersForStorage(kept)
	character.GuildID = nil
	door.s.changes.guilds[guildKey{division: divisionID, guildID: guildID}] = true
	door.s.changes.characters[character] = true
	door.s.commitLocked(fmt.Sprintf("%s %s/%d", label, divisionID, guildID))
	return domain.GuildRemovalResult{
		Guild:         guild,
		RemovedMember: target,
		MembersBefore: membersBefore,
	}, domain.GuildRefusalNone
}

// guildMemberViewsLocked projects character-owned roster fields while the
// store lock keeps membership and character state coherent. Stored guild rows
// deliberately carry no mutable character level.
func (door storeGuildDoor) guildMemberViewsLocked(
	divisionID string,
	members []domain.GuildMemberRecord,
) ([]domain.GuildMemberRecord, bool) {
	views := make([]domain.GuildMemberRecord, len(members))
	for index, member := range members {
		character := door.s.characterByIDLocked(divisionID, member.CharID)
		if character == nil {
			return nil, false
		}
		member.Level = guildMemberLevel(character)
		views[index] = member
	}
	return views, true
}

// guildMembersForStorage strips character-owned projections before a member
// set becomes live or durable. This makes accidental stale-level retention
// impossible even in the in-memory aggregate.
func guildMembersForStorage(members []domain.GuildMemberRecord) []domain.GuildMemberRecord {
	stored := make([]domain.GuildMemberRecord, len(members))
	for index, member := range members {
		member.Level = 0
		stored[index] = member
	}
	return stored
}

func guildMemberLevel(character *domain.Character) uint8 {
	level := resolvedCharacterLevel(character)
	if level > 0xff {
		level = 0xff
	}
	return uint8(level)
}

// maxGuildIDLocked is the watermark safety net for an unseeded division
// (maxCharIDLocked's twin): live guild rows only - a division that has
// ever allocated through the watermark carries the row instead.
func (s *Store) maxGuildIDLocked(divisionID string) int64 {
	max := int64(0)
	for guildID := range s.guilds[divisionID] {
		if guildID > max {
			max = guildID
		}
	}
	return max
}
