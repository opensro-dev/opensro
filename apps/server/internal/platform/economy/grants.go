/*
===========================================================================

grants.go - the free grants route: what each account was handed (#3307)

GET /economy/v1/grants?from=&to=&account= answers the store's free-grant
days (store/grant_days.go) inside a window of whole UTC days: per
account, day, grant and item, the total granted and how many times.
account narrows it to one account; without it the busiest rows come
first, so a farm of fresh accounts reads at the top.

The answer is read from the latest snapshot copy, like every economy
route: it can trail the live store by one snapshot period
(SRO_ECONOMY_SNAPSHOT_HOURS, 6 by default), and snapshotAt says when.

===========================================================================
*/
package economy

import (
	"net/http"
	"net/url"
	"path/filepath"
	"time"
)

// maxGrantRows bounds one answer; truncated says when more matched.
const maxGrantRows = 1000

/*
================
GrantDay

One account's grant of one item on one UTC day (unix seconds).
================
*/
type GrantDay struct {
	Day     int64  `json:"day"`
	Account string `json:"account"`
	Source  string `json:"source"`
	Item    string `json:"item"`
	Total   int64  `json:"total"`
	N       int64  `json:"n"`
}

/*
================
Service.serveGrants
================
*/
func (s *Service) serveGrants(w http.ResponseWriter, q url.Values, at time.Time) {
	win, err := parseWindow("1d", q.Get("from"), q.Get("to"), "", s.now())
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	grants, truncated, err := s.readGrants(win, q.Get("account"))
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, map[string]any{"snapshotAt": at.Unix(), "grants": grants, "truncated": truncated})
}

/*
================
Service.readGrants

The window's grant days from the snapshot copy, one account's oldest
first, or every account's largest first.
================
*/
func (s *Service) readGrants(win window, account string) ([]GrantDay, bool, error) {
	db, err := openSnapshot(filepath.Join(s.dir, SnapshotFile))
	if err != nil {
		return nil, false, err
	}
	defer db.Close()
	query := `SELECT day_ms, account_id, source, item, total, n FROM grant_account_days
WHERE day_ms >= ? AND day_ms <= ?`
	args := []any{win.from * 1000, win.to * 1000}
	if account != "" {
		query += ` AND account_id = ? ORDER BY day_ms, source, item`
		args = append(args, account)
	} else {
		query += ` ORDER BY total DESC, day_ms, account_id, source, item`
	}
	query += ` LIMIT ?`
	args = append(args, maxGrantRows+1)
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	grants := []GrantDay{}
	for rows.Next() {
		var grant GrantDay
		if err := rows.Scan(&grant.Day, &grant.Account, &grant.Source, &grant.Item, &grant.Total, &grant.N); err != nil {
			return nil, false, err
		}
		grant.Day /= 1000
		grants = append(grants, grant)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	if len(grants) > maxGrantRows {
		return grants[:maxGrantRows], true, nil
	}
	return grants, false, nil
}
