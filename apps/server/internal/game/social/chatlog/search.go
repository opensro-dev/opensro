/*
===========================================================================

search.go - the staff console's read of the moderation chat log

Port-only, not native. Search walks the daily files newest first, so a
bounded answer always holds the most recent matches. The handler answers
only through the shard's operator guard (operatorauth), the rule every
private read shares, and only to the staff console on the same host;
nothing here is reachable from the public site or the public API. Chat
text is what players typed: it is returned as data and the console paints
it as text.

===========================================================================
*/
package chatlog

import (
	"bufio"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"opensro.online/server/internal/platform/operatorauth"
)

// OperatorPath is the console's read route on the local operator API.
const OperatorPath = "/internal/operations/chatlog"

const (
	// DefaultSearchLimit and MaxSearchLimit bound one answer's lines.
	DefaultSearchLimit = 200
	MaxSearchLimit     = 500
	// maxFilterLength bounds a name filter, as the console's history does.
	maxFilterLength = 256
	// maxLineBytes bounds one stored line; the writer's text is far smaller.
	maxLineBytes = 1 << 20
)

/*
================
Query

One search. Empty names and zero times do not filter; From is inclusive,
To exclusive. A non-positive Limit takes the default.
================
*/
type Query struct {
	Account   string
	Character string
	From      time.Time
	To        time.Time
	Limit     int
}

/*
================
Result

The matches, newest first. Truncated says older matches exist past the
limit. Skipped counts unreadable lines, so damage never reads as silence.
================
*/
type Result struct {
	Lines     []line `json:"lines"`
	Truncated bool   `json:"truncated"`
	Skipped   int    `json:"skipped"`
}

/*
================
matches
================
*/
func (q Query) matches(entry line, at time.Time) bool {
	if q.Account != "" && !strings.EqualFold(entry.Account, q.Account) {
		return false
	}
	if q.Character != "" && !strings.EqualFold(entry.Character, q.Character) {
		return false
	}
	if !q.From.IsZero() && at.Before(q.From) {
		return false
	}
	return q.To.IsZero() || at.Before(q.To)
}

/*
================
overlaps

Whether a daily file can hold a line of the query's window.
================
*/
func (q Query) overlaps(day time.Time) bool {
	if !q.From.IsZero() && !day.AddDate(0, 0, 1).After(q.From) {
		return false
	}
	return q.To.IsZero() || day.Before(q.To)
}

/*
================
dailyFiles

The query's daily files in dir, newest first. A missing directory is an
empty log: the shard has recorded nothing yet.
================
*/
func dailyFiles(dir string, q Query) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var names []string
	for _, entry := range entries {
		name := entry.Name()
		date, ok := strings.CutPrefix(name, FilePrefix)
		if !ok {
			continue
		}
		date, ok = strings.CutSuffix(date, FileSuffix)
		if !ok {
			continue
		}
		day, err := time.Parse(dateLayout, date)
		if err == nil && q.overlaps(day) {
			names = append(names, name)
		}
	}
	// The date is the sort key: the prefix and the layout are fixed width.
	slices.Sort(names)
	slices.Reverse(names)
	return names, nil
}

/*
================
searchFile

The file's matching lines in file order, and how many it could not read.
The writer appends while this reads, so a torn last line counts as
skipped and reads whole on the next search.
================
*/
func searchFile(path string, q Query) ([]line, int, error) {
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		// Retention pruned it after the directory listing.
		return nil, 0, nil
	}
	if err != nil {
		return nil, 0, err
	}
	defer file.Close()
	var (
		found   []line
		skipped int
	)
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, writeBuffer), maxLineBytes)
	for scanner.Scan() {
		var entry line
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			skipped++
			continue
		}
		at, err := time.Parse(time.RFC3339, entry.TS)
		if err != nil {
			skipped++
			continue
		}
		if q.matches(entry, at) {
			found = append(found, entry)
		}
	}
	return found, skipped, scanner.Err()
}

/*
================
Search
================
*/
func Search(dir string, q Query) (Result, error) {
	limit := q.Limit
	if limit <= 0 {
		limit = DefaultSearchLimit
	}
	limit = min(limit, MaxSearchLimit)
	names, err := dailyFiles(dir, q)
	if err != nil {
		return Result{}, err
	}
	result := Result{Lines: []line{}}
	for _, name := range names {
		found, skipped, err := searchFile(filepath.Join(dir, name), q)
		if err != nil {
			return Result{}, err
		}
		result.Skipped += skipped
		slices.Reverse(found)
		if room := limit - len(result.Lines); len(found) > room {
			found = found[:room]
			result.Truncated = true
		}
		result.Lines = append(result.Lines, found...)
		if result.Truncated {
			break
		}
	}
	return result, nil
}

/*
================
millis

An optional Unix-millisecond filter; absent is the zero time.
================
*/
func millis(values url.Values, name string) (time.Time, error) {
	text := values.Get(name)
	if text == "" {
		return time.Time{}, nil
	}
	value, err := strconv.ParseInt(text, 10, 64)
	if err != nil || value < 0 {
		return time.Time{}, errors.New(name + " must be Unix milliseconds")
	}
	return time.UnixMilli(value), nil
}

/*
================
parseQuery
================
*/
func parseQuery(values url.Values) (Query, error) {
	q := Query{Account: values.Get("account"), Character: values.Get("character")}
	if len(q.Account) > maxFilterLength || len(q.Character) > maxFilterLength {
		return Query{}, errors.New("filter is too long")
	}
	var err error
	if q.From, err = millis(values, "from"); err != nil {
		return Query{}, err
	}
	if q.To, err = millis(values, "to"); err != nil {
		return Query{}, err
	}
	if text := values.Get("limit"); text != "" {
		if q.Limit, err = strconv.Atoi(text); err != nil || q.Limit <= 0 {
			return Query{}, errors.New("limit must be a positive number")
		}
	}
	return q, nil
}

/*
================
OperatorHandler

The console's search. With no token file the route answers nobody, as
every operator read does; a valid token from a browser origin or through
a forward is still refused by the guard.
================
*/
func OperatorHandler(dir, tokenPath string) (http.Handler, error) {
	guard, found, err := operatorauth.Load(tokenPath, "chatlog")
	if err != nil {
		return nil, err
	}
	if !found {
		return http.NotFoundHandler(), nil
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !guard.Allows(r) {
			http.Error(w, "operator authentication required", http.StatusForbidden)
			return
		}
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		q, err := parseQuery(r.URL.Query())
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		result, err := Search(dir, q)
		if err != nil {
			http.Error(w, "chat log unavailable", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(result)
	}), nil
}
