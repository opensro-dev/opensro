/*
===========================================================================

guild_leader_role.go - the offline repair that gives each master the
commander role exclusively to the master

The retail _Guild_FnAddMember gives MemberClass 0 SiegeAuthority 1, and a
master handover moves it (v1.188 5C46E0, _Guild_Delegate_Master). Guilds
founded before the port did the same carry a master with role 0, whom the
client's fortress windows (827DB0) refuse while the server's grade checks
admit. The authority upgrade rewrites those rows once; server startup
never does. The old grant handler also allowed role 1 on nonmasters;
clear those stale commanders while preserving legitimate staff roles.

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

One guild_members row whose commander role disagrees with its grade, with the
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

The rows the upgrade must rewrite. The master alone holds role 1;
all other legitimate roles survive. Correct rows are left alone.
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
		role := member.FortressRole
		if member.Grade == domain.GuildLeaderGrade {
			role = domain.GuildFortressRoleCommander
		} else if role == domain.GuildFortressRoleCommander {
			role = 0
		}
		if member.FortressRole == role {
			continue
		}
		member.FortressRole = role
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
