/*
===========================================================================

query.go - bounded operator searches and overlap-safe online totals

===========================================================================
*/
package history

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

/*
================
Filter
================
*/
type Filter struct {
	Account, Character, Session, Incident, Category, Build, Opcode, Kind, Search string
	Before, From, To                                                             int64
	Limit                                                                        int
}

/*
================
EventRow
================
*/
type EventRow struct {
	Sequence int64 `json:"sequence"`
	Event
}

/*
================
SessionRow
================
*/
type SessionRow struct {
	ID               string  `json:"id"`
	Account          string  `json:"account"`
	Character        string  `json:"character"`
	Shard            string  `json:"shard"`
	Started          int64   `json:"started"`
	Seen             int64   `json:"seen"`
	Ended            int64   `json:"ended"`
	Attached         bool    `json:"attached"`
	Category         string  `json:"category"`
	Code             string  `json:"code"`
	Estimated        bool    `json:"estimated"`
	ConnectedSeconds float64 `json:"connectedSeconds"`
}

/*
================
Fingerprint

Build stays separate so the same failure can be compared across releases.
Structured code/opcode groups typed incidents; generic errors use the message.
================
*/
func Fingerprint(e Event) string {
	text := e.Service + "|" + e.Kind + "|" + e.Code + "|" + e.Opcode
	if e.Code == "" {
		text += "|" + e.Message
	}
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:12])
}

/*
================
where
================
*/
func where(f Filter) (string, []any) {
	parts := []string{"1=1"}
	args := []any{}
	if f.Account != "" {
		parts = append(parts, "(account=? COLLATE NOCASE OR (kind='login_refused' AND json_extract(data,'$.fields.claimedAccount')=? COLLATE NOCASE))")
		args = append(args, f.Account, f.Account)
	}
	for _, pair := range [][2]string{{"character", f.Character}, {"session", f.Session}, {"id", f.Incident}, {"category", f.Category}, {"kind", f.Kind}} {
		if pair[1] != "" {
			parts = append(parts, pair[0]+" = ? COLLATE NOCASE")
			args = append(args, pair[1])
		}
	}
	for _, pair := range [][2]string{{"build", f.Build}, {"opcode", f.Opcode}} {
		if pair[1] != "" {
			parts = append(parts, "json_extract(data,'$."+pair[0]+"') = ?")
			args = append(args, pair[1])
		}
	}
	if f.Search != "" {
		parts = append(parts, "instr(lower(json_extract(data,'$.message')),lower(?))>0")
		args = append(args, f.Search)
	}
	if f.Before > 0 {
		parts = append(parts, "seq<?")
		args = append(args, f.Before)
	}
	if f.From > 0 {
		parts = append(parts, "at>=?")
		args = append(args, f.From)
	}
	if f.To > 0 {
		parts = append(parts, "at<=?")
		args = append(args, f.To)
	}
	return strings.Join(parts, " AND "), args
}

/*
================
Query
================
*/
func (j *Journal) Query(f Filter) (map[string]any, error) {
	if f.Limit < 1 || f.Limit > 200 {
		f.Limit = 100
	}
	clause, args := where(f)
	rows, err := j.db.Query("SELECT seq,id,data FROM events WHERE "+clause+" ORDER BY seq DESC LIMIT ?", append(args, f.Limit+1)...)
	if err != nil {
		return nil, err
	}
	events := []EventRow{}
	for rows.Next() {
		var e EventRow
		var data, id string
		if err = rows.Scan(&e.Sequence, &id, &data); err != nil {
			break
		}
		if err = json.Unmarshal([]byte(data), &e.Event); err != nil {
			break
		}
		e.ID = id
		events = append(events, e)
	}
	err = firstError(err, rows.Err(), rows.Close())
	if err != nil {
		return nil, err
	}
	var next int64
	if len(events) > f.Limit {
		events = events[:f.Limit]
		next = events[len(events)-1].Sequence
	}
	sessions, err := j.sessions(f)
	if err != nil {
		return nil, err
	}
	groups, err := j.groups(f)
	if err != nil {
		return nil, err
	}
	summary, err := j.summary(f, time.Now())
	if err != nil {
		return nil, err
	}
	return map[string]any{"events": events, "next": next, "sessions": sessions, "groups": groups, "summary": summary, "health": j.Health()}, nil
}

/*
================
firstError
================
*/
func firstError(errors ...error) error {
	for _, err := range errors {
		if err != nil {
			return err
		}
	}
	return nil
}

/*
================
sessions
================
*/
func (j *Journal) sessions(f Filter) ([]SessionRow, error) {
	rows, err := j.db.Query(`SELECT s.id,s.account,s.character,s.shard,s.started,s.seen,s.ended,s.attached,s.category,s.code,s.estimated,
 COALESCE((SELECT SUM(finish-start)/1000.0 FROM intervals WHERE session=s.id),0)
 FROM sessions s WHERE (?='' OR account=? COLLATE NOCASE) AND (?='' OR character=? COLLATE NOCASE)
 AND (?='' OR id=?) ORDER BY started DESC LIMIT 100`, f.Account, f.Account, f.Character, f.Character, f.Session, f.Session)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []SessionRow{}
	for rows.Next() {
		var row SessionRow
		if err := rows.Scan(&row.ID, &row.Account, &row.Character, &row.Shard, &row.Started, &row.Seen, &row.Ended, &row.Attached, &row.Category, &row.Code, &row.Estimated, &row.ConnectedSeconds); err != nil {
			return nil, err
		}
		result = append(result, row)
	}
	return result, rows.Err()
}

/*
================
groups
================
*/
func (j *Journal) groups(f Filter) ([]map[string]any, error) {
	f.Before = 0
	clause, args := where(f)
	rows, err := j.db.Query(`SELECT fingerprint,MIN(at),MAX(at),COUNT(*),COUNT(DISTINCT NULLIF(account,'')),
 json_extract(data,'$.code'),json_extract(data,'$.message'),GROUP_CONCAT(DISTINCT json_extract(data,'$.build'))
 FROM events WHERE (`+clause+`) AND (category='software' OR level IN ('error','fatal','panic'))
 GROUP BY fingerprint ORDER BY MAX(at) DESC LIMIT 100`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []map[string]any{}
	for rows.Next() {
		var id, code, message, builds string
		var first, last, count, players int64
		if err := rows.Scan(&id, &first, &last, &count, &players, &code, &message, &builds); err != nil {
			return nil, err
		}
		result = append(result, map[string]any{"id": id, "first": first, "last": last, "count": count, "players": players, "code": code, "message": message, "builds": builds})
	}
	if err := firstError(rows.Err(), rows.Close()); err != nil {
		return nil, err
	}
	buildRows, err := j.db.Query(`SELECT fingerprint,json_extract(data,'$.build'),COUNT(*) FROM events WHERE (`+clause+`) AND (category='software' OR level IN ('error','fatal','panic')) GROUP BY fingerprint,json_extract(data,'$.build')`, args...)
	if err != nil {
		return nil, err
	}
	defer buildRows.Close()
	counts := map[string]map[string]int64{}
	for buildRows.Next() {
		var fingerprint, build string
		var count int64
		if err := buildRows.Scan(&fingerprint, &build, &count); err != nil {
			return nil, err
		}
		if counts[fingerprint] == nil {
			counts[fingerprint] = map[string]int64{}
		}
		counts[fingerprint][build] = count
	}
	for _, group := range result {
		group["buildCounts"] = counts[group["id"].(string)]
	}
	return result, buildRows.Err()
}

/*
================
summary

Only a selected identity gets a playtime total. Merge overlapping connection
intervals before summing; simultaneous sessions never double-count wall time.
================
*/
func (j *Journal) summary(f Filter, now time.Time) (map[string]any, error) {
	result := map[string]any{"definition": "Connected world time, not active playtime. UTC day/week. Checkpointed every 15 seconds."}
	if f.Account == "" && f.Character == "" {
		return result, nil
	}
	day := time.Date(now.UTC().Year(), now.UTC().Month(), now.UTC().Day(), 0, 0, 0, 0, time.UTC)
	week := day.AddDate(0, 0, -(int(day.Weekday())+6)%7)
	var all, today, weekly float64
	err := j.db.QueryRow(`WITH ordered AS (
 SELECT start,finish,MAX(finish) OVER(ORDER BY start,finish ROWS BETWEEN UNBOUNDED PRECEDING AND 1 PRECEDING) previous
 FROM intervals WHERE (?='' OR account=? COLLATE NOCASE) AND (?='' OR character=? COLLATE NOCASE)),
 pieces AS (SELECT MAX(start,COALESCE(previous,start)) start,finish FROM ordered)
 SELECT COALESCE(SUM(MAX(0,finish-start)),0)/1000.0,
 COALESCE(SUM(MAX(0,finish-MAX(start,?))),0)/1000.0,
 COALESCE(SUM(MAX(0,finish-MAX(start,?))),0)/1000.0 FROM pieces`,
		f.Account, f.Account, f.Character, f.Character, day.UnixMilli(), week.UnixMilli()).Scan(&all, &today, &weekly)
	if err != nil {
		return nil, err
	}
	var lastLogin, lastSeen, sessionCount int64
	err = j.db.QueryRow(`SELECT COALESCE(MAX(started),0),COALESCE(MAX(seen),0),COUNT(*) FROM sessions
 WHERE (?='' OR account=? COLLATE NOCASE) AND (?='' OR character=? COLLATE NOCASE)`, f.Account, f.Account, f.Character, f.Character).Scan(&lastLogin, &lastSeen, &sessionCount)
	if err != nil {
		return nil, err
	}
	var authentication int64
	err = j.db.QueryRow(`SELECT COALESCE(MAX(at),0) FROM logins WHERE account=? COLLATE NOCASE`, f.Account).Scan(&authentication)
	if err != nil {
		return nil, err
	}
	result["connectedSeconds"], result["todaySeconds"], result["weekSeconds"] = all, today, weekly
	result["lastWorldSession"], result["lastSeen"], result["sessions"], result["lastAuthentication"] = lastLogin, lastSeen, sessionCount, authentication
	return result, nil
}

/*
================
ServeHTTP

The service composition wraps this read-only endpoint in operator auth.
================
*/
func (j *Journal) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	q := r.URL.Query()
	f := Filter{Account: q.Get("account"), Character: q.Get("character"), Session: q.Get("session"), Incident: q.Get("incident"), Category: q.Get("category"), Build: q.Get("build"), Opcode: q.Get("opcode"), Kind: q.Get("kind"), Search: q.Get("q")}
	for _, v := range q {
		for _, s := range v {
			if len(s) > 256 {
				http.Error(w, "filter too long", http.StatusBadRequest)
				return
			}
		}
	}
	for key, dest := range map[string]*int64{"before": &f.Before, "from": &f.From, "to": &f.To} {
		if q.Get(key) == "" {
			continue
		}
		value, err := strconv.ParseInt(q.Get(key), 10, 64)
		if err != nil || value < 0 {
			http.Error(w, "invalid cursor or time", http.StatusBadRequest)
			return
		}
		*dest = value
	}
	data, err := j.Query(f)
	if err != nil {
		http.Error(w, fmt.Sprintf("history unavailable: %v", err), http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(data)
}
