/*
===========================================================================

book.go - the staff book: who is staff, with which role and powers

Port-only, not native; the default by owner decision 2026-11-23. The
book is the authority over console-granted staff. It is written to the
shard's authority directory on every change (staff-grants.json), opened
at boot before admission, and a corrupt file refuses the boot: silently
forgetting every grant, or every revoke, must not happen unnoticed.

A role is presentation data staff edit in the console: the tag drawn
before the name and the two colours (name, chat). The seeded "gm" role
wears the original's GM look: the client paints a name starting with
"[GM]" 0xFFFFD87A (v1.150 00858848) and a GM chat line, type 3,
0xFFFFAEC3 (the 752A2B colour table).

A grant names a character, a role and a capability set (capabilities.go).
The book never touches characters: the game world projects a grant onto
Character.GMPrivilege and GMCapabilities (action OperatorSetStaff) and
tells the clients. The host allowlist (SRO_GM_CHARACTERS) is a separate,
read-only source the book never writes.

===========================================================================
*/
package staff

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
)

// FileName is the book's file in the shard's authority directory.
const FileName = "staff-grants.json"

const (
	bookFileVersion  = 1
	maxTagRunes      = 12
	maxOperatorBytes = 80
	maxRoles         = 32
	maxCharacterLen  = 64
)

// GMRole is the seeded role id, wearing the original's GM colours.
const GMRole = "gm"

var (
	roleIDPattern = regexp.MustCompile(`^[a-z0-9-]{1,16}$`)
	colorPattern  = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)

	// ErrNoGrant is returned when a revoke names a character without one.
	ErrNoGrant = errors.New("staff: character holds no staff grant")
	// ErrElevated is returned when a non-approver adds an elevated power.
	ErrElevated = errors.New("staff: only an approver may grant an elevated capability")
)

/*
================
Role
================
*/
type Role struct {
	ID        string `json:"id"`
	Tag       string `json:"tag"`
	NameColor string `json:"nameColor"`
	ChatColor string `json:"chatColor"`
}

/*
================
Grant
================
*/
type Grant struct {
	Division     string   `json:"division"`
	Character    string   `json:"character"`
	Role         string   `json:"role"`
	Capabilities []string `json:"capabilities"`
	Operator     string   `json:"operator"`
	AtMs         int64    `json:"atMs"`
}

/*
================
Request

One grant or update from the console. Approver is the console role of
the signed-in staff member, which the console's edge identity proves.
================
*/
type Request struct {
	Division     string
	Character    string
	Role         string
	Capabilities []string
	Operator     string
	Approver     bool
}

/*
================
RosterEntry

What every client is told about one staff character, online or not.
================
*/
type RosterEntry struct {
	Character string
	Tag       string
	NameColor uint32
	ChatColor uint32
}

type bookFile struct {
	Version int     `json:"version"`
	Roles   []Role  `json:"roles"`
	Grants  []Grant `json:"grants"`
}

/*
================
Book
================
*/
type Book struct {
	mu     sync.Mutex
	path   string
	now    func() time.Time
	write  func(path string, payload []byte) error
	roles  map[string]Role
	grants map[string]Grant
}

func key(division, character string) string {
	return division + ":" + strings.ToLower(character)
}

func seedRole() Role {
	return Role{ID: GMRole, Tag: "[GM]", NameColor: "#FFD87A", ChatColor: "#FFAEC3"}
}

/*
================
OpenBook

Loads the book from path (a missing file is an empty book with the seeded
gm role); write is the durable atomic writer (store.WriteFileAtomic).
================
*/
func OpenBook(path string, now func() time.Time, write func(string, []byte) error) (*Book, error) {
	if now == nil || write == nil {
		return nil, errors.New("staff: clock and writer are required")
	}
	book := &Book{path: path, now: now, write: write, roles: map[string]Role{}, grants: map[string]Grant{}}
	raw, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("staff: read %s: %w", path, err)
	}
	if err == nil {
		var file bookFile
		if err := json.Unmarshal(raw, &file); err != nil || file.Version != bookFileVersion {
			return nil, fmt.Errorf("staff: %s is not a version %d staff book (%v)", path, bookFileVersion, err)
		}
		for _, role := range file.Roles {
			if err := validRole(role); err != nil {
				return nil, fmt.Errorf("staff: %s: %w", path, err)
			}
			book.roles[role.ID] = role
		}
		for _, grant := range file.Grants {
			if _, err := Mask(grant.Capabilities); err != nil {
				return nil, fmt.Errorf("staff: %s: %w", path, err)
			}
			book.grants[key(grant.Division, grant.Character)] = grant
		}
	}
	if _, ok := book.roles[GMRole]; !ok {
		book.roles[GMRole] = seedRole()
	}
	for _, grant := range book.grants {
		if _, ok := book.roles[grant.Role]; !ok {
			return nil, fmt.Errorf("staff: %s: grant for %s names unknown role %q", path, grant.Character, grant.Role)
		}
	}
	return book, nil
}

/*
================
Roles
================
*/
func (book *Book) Roles() []Role {
	book.mu.Lock()
	defer book.mu.Unlock()
	roles := make([]Role, 0, len(book.roles))
	for _, role := range book.roles {
		roles = append(roles, role)
	}
	sort.Slice(roles, func(i, j int) bool { return roles[i].ID < roles[j].ID })
	return roles
}

/*
================
SetRole

Adds or edits a role (colours are stored upper case). The caller re-sends
the roster, since every grant wearing the role changes look.
================
*/
func (book *Book) SetRole(role Role) (Role, error) {
	role.NameColor, role.ChatColor = strings.ToUpper(role.NameColor), strings.ToUpper(role.ChatColor)
	if err := validRole(role); err != nil {
		return Role{}, err
	}
	book.mu.Lock()
	defer book.mu.Unlock()
	previous, existed := book.roles[role.ID]
	if !existed && len(book.roles) >= maxRoles {
		return Role{}, fmt.Errorf("staff: at most %d roles", maxRoles)
	}
	book.roles[role.ID] = role
	if err := book.saveLocked(); err != nil {
		if existed {
			book.roles[role.ID] = previous
		} else {
			delete(book.roles, role.ID)
		}
		return Role{}, err
	}
	return role, nil
}

/*
================
Grant

Adds or replaces one character's grant. A non-approver may keep or drop
an elevated capability the grant already holds, never add one.
================
*/
func (book *Book) Grant(request Request) (Grant, Capability, error) {
	if request.Division == "" || request.Character == "" || len(request.Character) > maxCharacterLen {
		return Grant{}, 0, errors.New("staff: a division and a character are required")
	}
	if request.Operator == "" || len(request.Operator) > maxOperatorBytes {
		return Grant{}, 0, errors.New("staff: the operator is required")
	}
	mask, err := Mask(request.Capabilities)
	if err != nil {
		return Grant{}, 0, err
	}
	book.mu.Lock()
	defer book.mu.Unlock()
	if _, ok := book.roles[request.Role]; !ok {
		return Grant{}, 0, fmt.Errorf("staff: unknown role %q", request.Role)
	}
	k := key(request.Division, request.Character)
	previous, existed := book.grants[k]
	var held Capability
	if existed {
		held, _ = Mask(previous.Capabilities)
	}
	if !request.Approver && Elevated(mask)&^held != 0 {
		return Grant{}, 0, ErrElevated
	}
	grant := Grant{Division: request.Division, Character: request.Character, Role: request.Role,
		Capabilities: IDs(mask), Operator: request.Operator, AtMs: book.now().UnixMilli()}
	book.grants[k] = grant
	if err := book.saveLocked(); err != nil {
		if existed {
			book.grants[k] = previous
		} else {
			delete(book.grants, k)
		}
		return Grant{}, 0, err
	}
	return grant, mask, nil
}

/*
================
Revoke
================
*/
func (book *Book) Revoke(division, character string) (Grant, error) {
	book.mu.Lock()
	defer book.mu.Unlock()
	k := key(division, character)
	grant, ok := book.grants[k]
	if !ok {
		return Grant{}, ErrNoGrant
	}
	delete(book.grants, k)
	if err := book.saveLocked(); err != nil {
		book.grants[k] = grant
		return Grant{}, err
	}
	return grant, nil
}

/*
================
Grants

The division's grants, by character name.
================
*/
func (book *Book) Grants(division string) []Grant {
	book.mu.Lock()
	defer book.mu.Unlock()
	var grants []Grant
	for _, grant := range book.grants {
		if grant.Division == division {
			grants = append(grants, grant)
		}
	}
	sort.Slice(grants, func(i, j int) bool {
		return strings.ToLower(grants[i].Character) < strings.ToLower(grants[j].Character)
	})
	return grants
}

/*
================
Lookup
================
*/
func (book *Book) Lookup(division, character string) (Grant, bool) {
	book.mu.Lock()
	defer book.mu.Unlock()
	grant, ok := book.grants[key(division, character)]
	return grant, ok
}

/*
================
Roster

Every granted character of the division with its role's tag and colours,
as the clients draw them. A host-allowlist GM holds no book grant and
wears the seeded gm role, so the caller passes those names in.
================
*/
func (book *Book) Roster(division string, allowlisted []string) []RosterEntry {
	book.mu.Lock()
	defer book.mu.Unlock()
	seen := map[string]bool{}
	var roster []RosterEntry
	for _, grant := range book.grants {
		if grant.Division != division {
			continue
		}
		seen[strings.ToLower(grant.Character)] = true
		roster = append(roster, rosterEntry(grant.Character, book.roles[grant.Role]))
	}
	for _, name := range allowlisted {
		if !seen[strings.ToLower(name)] {
			seen[strings.ToLower(name)] = true
			roster = append(roster, rosterEntry(name, book.roles[GMRole]))
		}
	}
	sort.Slice(roster, func(i, j int) bool {
		return strings.ToLower(roster[i].Character) < strings.ToLower(roster[j].Character)
	})
	return roster
}

func rosterEntry(character string, role Role) RosterEntry {
	return RosterEntry{Character: character, Tag: role.Tag, NameColor: argb(role.NameColor), ChatColor: argb(role.ChatColor)}
}

// argb reads a validated "#RRGGBB" as the clients' opaque ARGB.
func argb(color string) uint32 {
	rgb, _ := strconv.ParseUint(strings.TrimPrefix(color, "#"), 16, 32)
	return 0xFF000000 | uint32(rgb)
}

func (book *Book) saveLocked() error {
	file := bookFile{Version: bookFileVersion}
	for _, role := range book.roles {
		file.Roles = append(file.Roles, role)
	}
	sort.Slice(file.Roles, func(i, j int) bool { return file.Roles[i].ID < file.Roles[j].ID })
	for _, grant := range book.grants {
		file.Grants = append(file.Grants, grant)
	}
	sort.Slice(file.Grants, func(i, j int) bool {
		return key(file.Grants[i].Division, file.Grants[i].Character) < key(file.Grants[j].Division, file.Grants[j].Character)
	})
	payload, err := json.MarshalIndent(file, "", "\t")
	if err != nil {
		return err
	}
	if err := book.write(book.path, append(payload, '\n')); err != nil {
		return fmt.Errorf("staff: save %s: %w", book.path, err)
	}
	return nil
}

/*
================
validRole

A role id is short lower-case ASCII; the tag is 1 to maxTagRunes
printable characters (no control or line breaks, so it cannot forge a
chat line); colours are "#RRGGBB".
================
*/
func validRole(role Role) error {
	if !roleIDPattern.MatchString(role.ID) {
		return fmt.Errorf("staff: role id %q must be 1 to 16 of a-z, 0-9 and -", role.ID)
	}
	tag := strings.TrimSpace(role.Tag)
	if tag == "" || tag != role.Tag || !utf8.ValidString(tag) || utf8.RuneCountInString(tag) > maxTagRunes {
		return fmt.Errorf("staff: role %s tag must be 1 to %d characters with no outer spaces", role.ID, maxTagRunes)
	}
	for _, r := range tag {
		if !unicode.IsPrint(r) {
			return fmt.Errorf("staff: role %s tag holds a control character", role.ID)
		}
	}
	if !colorPattern.MatchString(role.NameColor) || !colorPattern.MatchString(role.ChatColor) {
		return fmt.Errorf("staff: role %s colours must be #RRGGBB", role.ID)
	}
	return nil
}
