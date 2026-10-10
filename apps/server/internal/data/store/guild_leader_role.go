/*
===========================================================================

guild_leader_role.go - the offline repair that gives each master the
commander role

The retail _Guild_FnAddMember gives MemberClass 0 SiegeAuthority 1, and a
master handover moves it (v1.188 5C46E0, _Guild_Delegate_Master). Guilds
founded before the port did the same carry a master with role 0, whom the
client's fortress windows (827DB0) refuse while the server's grade checks
admit. The authority upgrade rewrites those rows once; server startup
never does.

===========================================================================
*/
package store

import (
	"database/sql"
	"encoding/json"
	"fmt"

	"opensro.online/server/internal/domain"
)

/*
================
leaderRoleRepair

One guild_members row whose master lacks the commander role, with the
record as it will be written.
================
*/
type leaderRoleRepair struct {
	division string
	guildID  int64
	charID   int64
	record   string
}

/*
================
leaderRoleRepairs

The master rows the upgrade must rewrite. A row already holding the role
is left alone, so the repair is idempotent.
================
*/
func leaderRoleRepairs(db *sql.DB) ([]leaderRoleRepair, error) {
	rows, err := db.Query("SELECT division, guild_id, char_id, record FROM guild_members ORDER BY division, guild_id, seq")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var repairs []leaderRoleRepair
	for rows.Next() {
		var repair leaderRoleRepair
		var record string
		if err := rows.Scan(&repair.division, &repair.guildID, &repair.charID, &record); err != nil {
			return nil, err
		}
		member := domain.GuildMemberRecord{}
		if err := decodeJSONStrict([]byte(record), &member); err != nil {
			return nil, fmt.Errorf("division %s guild %d member record: %w", repair.division, repair.guildID, err)
		}
		if member.Grade != domain.GuildLeaderGrade || member.FortressRole == domain.GuildFortressRoleCommander {
			continue
		}
		member.FortressRole = domain.GuildFortressRoleCommander
		encoded, err := json.Marshal(member)
		if err != nil {
			return nil, err
		}
		repair.record = string(encoded)
		repairs = append(repairs, repair)
	}
	return repairs, rows.Err()
}

/*
================
applyLeaderRoleRepairs
================
*/
func applyLeaderRoleRepairs(tx *sql.Tx, repairs []leaderRoleRepair) error {
	for _, repair := range repairs {
		result, err := tx.Exec("UPDATE guild_members SET record = ? WHERE division = ? AND guild_id = ? AND char_id = ?",
			repair.record, repair.division, repair.guildID, repair.charID)
		if err != nil {
			return err
		}
		if changed, err := result.RowsAffected(); err != nil || changed != 1 {
			return fmt.Errorf("authority upgrade: master role row %s/%d/%d not rewritten", repair.division, repair.guildID, repair.charID)
		}
	}
	return nil
}
