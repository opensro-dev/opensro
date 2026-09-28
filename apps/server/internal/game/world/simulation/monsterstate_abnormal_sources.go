/*
===========================================================================

monsterstate_abnormal_sources.go - detached source facts for status effects

Combat resolves caster existence and life before entering either the character
mutation door or the monster population door. Status callbacks consume these
facts without re-entering either authority. The division operation lane keeps
planning and commit ordered; the ordinary impact checks still reject stale HP.

===========================================================================
*/
package simulation

import "opensro.online/server/internal/game/abnormal"

/*
================
MonsterAbnormalSource

One source's identity and life at the operation's read boundary.
================
*/
type MonsterAbnormalSource struct {
	Name   string
	Exists bool
	Dead   bool
}

/*
================
PrepareAbnormalSources

Must run before a character mutation closure. The context can read character
or monster authority, so no population lock may surround its callbacks.
================
*/
func (s *MonsterState) PrepareAbnormalSources(division string, records []abnormal.Record) map[uint32]MonsterAbnormalSource {
	if len(records) == 0 {
		return nil
	}
	s.mu.Lock()
	ctx := s.abnormalContext
	s.mu.Unlock()
	sources := make(map[uint32]MonsterAbnormalSource)
	for _, record := range records {
		if previous, found := sources[record.SourceGID]; found && previous.Name == record.SourceName {
			continue
		}
		source := MonsterAbnormalSource{Name: record.SourceName}
		if ctx != nil {
			source.Exists = ctx.SourceExists(division, record.SourceGID, record.SourceName)
			if source.Exists {
				source.Dead = ctx.SourceDead(division, record.SourceGID, record.SourceName)
			}
		}
		sources[record.SourceGID] = source
	}
	return sources
}

/*
================
validAbnormalSources

An unresolved source is a malformed plan, not permission to omit its status.
Validate the whole transaction before any victim loses HP.
================
*/
func validAbnormalSources(plan MonsterDamagePlan) bool {
	for _, record := range plan.Abnormal {
		source, ok := plan.AbnormalSources[record.SourceGID]
		if !ok || source.Name != record.SourceName {
			return false
		}
	}
	return true
}
