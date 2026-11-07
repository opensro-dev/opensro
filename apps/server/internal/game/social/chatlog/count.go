/*
===========================================================================

count.go - lines per character over a window, for the census

The player census (opensro#2717) asks one question of this log: how
much did each character say between two instants - its chat-silence
signal is "online for hours and said nothing". The count walks the
window's daily files once and keeps a tally per character; the lines
themselves stay here. Keys are the folded (lower-case) character
name, matching the case-insensitive name comparisons everywhere else.

===========================================================================
*/
package chatlog

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

/*
================
CountByCharacter

Accepted lines per character with From <= line < To; a zero bound is
open. skipped counts lines that could not be read, so damage never
reads as silence: the caller decides what a partial count means.
================
*/
func CountByCharacter(dir string, from, to time.Time) (counts map[string]int, skipped int, err error) {
	q := Query{From: from, To: to}
	names, err := dailyFiles(dir, q)
	if err != nil {
		return nil, 0, err
	}
	counts = map[string]int{}
	for _, name := range names {
		found, bad, err := countFile(filepath.Join(dir, name), q)
		if err != nil {
			return nil, skipped, err
		}
		skipped += bad
		for character, n := range found {
			counts[character] += n
		}
	}
	return counts, skipped, nil
}

/*
================
countFile

One daily file's tally. The writer appends while this reads, so a
torn last line counts as skipped and reads whole next time.
================
*/
func countFile(path string, q Query) (map[string]int, int, error) {
	file, err := os.Open(path)
	if os.IsNotExist(err) {
		// Retention pruned it after the directory listing.
		return nil, 0, nil
	}
	if err != nil {
		return nil, 0, err
	}
	defer file.Close()
	found := map[string]int{}
	skipped := 0
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
		if !q.From.IsZero() && at.Before(q.From) {
			continue
		}
		if !q.To.IsZero() && !at.Before(q.To) {
			continue
		}
		found[strings.ToLower(entry.Character)]++
	}
	return found, skipped, scanner.Err()
}
