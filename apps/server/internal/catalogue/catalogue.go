/*
===========================================================================

catalogue.go - the website's static game catalogue: shared types

Package catalogue turns the server's own reference data into the static
JSON the opensro.online database pages read (opensro-web docs/COMMUNITY.md,
section C). It reads the same loaders the server runs, so the pages show
what the server does. A Node build stage adds what only the client knows
(tooltip lines, web icons); this package never resolves .generated paths
itself.

Every entity carries a status. It is derived from the server's own
registries wherever one exists; where none does, the entity is
"unverified", never a guessed "implemented". A hand-written gap note is the
only manual input, kept in gaps.go with an issue link.

===========================================================================
*/
package catalogue

// SchemaVersion changes whenever a file's shape changes incompatibly.
const SchemaVersion = 1

/*
================
Status

implemented: the server's registry handles it; partial: the code knows of a
specific gap (see Gaps); missing: the registry has no handler; unverified:
no registry can say yet (shown as "not checked yet", never as progress).
================
*/
type Status string

const (
	StatusImplemented Status = "implemented"
	StatusPartial     Status = "partial"
	StatusMissing     Status = "missing"
	StatusUnverified  Status = "unverified"
)

// statusEnum is Status's JSON Schema enum.
var statusEnum = []string{string(StatusImplemented), string(StatusPartial), string(StatusMissing), string(StatusUnverified)}

/*
================
Standing

What every entity carries about how far the port has it: the status, the
gaps in player words when it is not implemented, and the issue tracking
them.
================
*/
type Standing struct {
	Status Status   `json:"status" enum:"status"`
	Gaps   []string `json:"gaps,omitempty"`
	Issue  string   `json:"issue,omitempty"`
}

/*
================
Header

The first fields of every catalogue file.
================
*/
type Header struct {
	SchemaVersion int    `json:"schemaVersion"`
	Release       string `json:"release"`
	GeneratedAt   string `json:"generatedAt"`
}
