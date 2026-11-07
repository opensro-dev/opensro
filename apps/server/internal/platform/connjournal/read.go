/*
===========================================================================

read.go - the connection journal's read side (opensro#2717)

Part 2 of the player census reads what part 1 recorded: every row
whose span starts at or after a bound, open rows included. The read
runs on the writer's own handle, so it can only ever see a committed
row; the census (internal/platform/census) does its arithmetic in
memory and never touches the file itself.

The hashes stay hashes here: Row carries ip_key and net_key because
correlation needs them, and the census is the only caller. Nothing in
this package knows how to turn a hash back into an address - there is
no way back, by design (key.go).

===========================================================================
*/
package connjournal

/*
================
Row

One connections row as the census reads it. Times are UTC
milliseconds; Ended is 0 while the session is still entered, and
Estimated marks a row a crash closed at its last checkpoint.
================
*/
type Row struct {
	Session   string
	Account   string
	Character string
	Shard     string
	Transport string
	Started   int64
	Seen      int64
	Ended     int64
	Estimated bool
	IPKey     string
	NetKey    string
	UserAgent string
	Build     string
	Screen    string
}

/*
================
Rows

Every row started at or after sinceMs, oldest first. A nil journal
has no rows and says so without an error: the census flag off is a
shard posture, not a read failure.
================
*/
func (j *Journal) Rows(sinceMs int64) ([]Row, error) {
	if j == nil {
		return nil, nil
	}
	rows, err := j.db.Query(`SELECT session, account, character, shard, transport,
 started, seen, ended, estimated, ip_key, net_key, user_agent, build, screen
 FROM connections WHERE started >= ? ORDER BY started, id`, sinceMs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Row
	for rows.Next() {
		var row Row
		var estimated int64
		if err := rows.Scan(&row.Session, &row.Account, &row.Character, &row.Shard, &row.Transport,
			&row.Started, &row.Seen, &row.Ended, &estimated, &row.IPKey, &row.NetKey,
			&row.UserAgent, &row.Build, &row.Screen); err != nil {
			return nil, err
		}
		row.Estimated = estimated != 0
		out = append(out, row)
	}
	return out, rows.Err()
}
