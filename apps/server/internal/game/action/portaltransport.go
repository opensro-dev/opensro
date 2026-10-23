/*
===========================================================================

portaltransport.go - which teleport links the server refuses a transport

The server reads a link's Restrict kind 2 (refused while a transport is
summoned) from its own database, not from the client's teleportlink.txt,
which carries the kind on the reverse links (the ferries rather than the
town gates). The operator's table (PortalTransportFile, from their own
server database) supplies the server's answer per link; a link it lacks,
or every link when there is no table, keeps the client's kind.

===========================================================================
*/
package action

import (
	"encoding/json"
	"fmt"

	"opensro.online/server/internal/gamedata"
)

// PortalTransportFile is the per-link transport restriction table in the
// projection's derived data.
const PortalTransportFile = "action/portal-transport.json"

// portalTransportFormat names the table's layout: [source, target, refuses]
// rows keyed by teleport codename.
const portalTransportFormat = "sro-portal-transport-v1"

/*
================
portalTransport

A link's (source, target) teleport codenames to whether the server refuses
it to a summoned transport. A nil table answers no link.
================
*/
type portalTransport map[[2]string]bool

/*
================
loadPortalTransport

The table from derivedDir, or nil when there is none.
================
*/
func loadPortalTransport(derivedDir string) (portalTransport, error) {
	data, err := gamedata.ReadDerived(derivedDir, PortalTransportFile)
	if err != nil || data == nil {
		return nil, err
	}
	var document struct {
		Format string
		Links  [][3]json.RawMessage
	}
	if err := json.Unmarshal(data, &document); err != nil {
		return nil, fmt.Errorf("portal transport: %w", err)
	}
	if document.Format != portalTransportFormat {
		return nil, fmt.Errorf("portal transport: format %q, want %q", document.Format, portalTransportFormat)
	}
	table := make(portalTransport, len(document.Links))
	for i, row := range document.Links {
		var key [2]string
		var refuses bool
		if json.Unmarshal(row[0], &key[0]) != nil || json.Unmarshal(row[1], &key[1]) != nil ||
			json.Unmarshal(row[2], &refuses) != nil || key[0] == "" || key[1] == "" {
			return nil, fmt.Errorf("portal transport: malformed link %d", i)
		}
		if _, exists := table[key]; exists {
			return nil, fmt.Errorf("portal transport: duplicate link %s->%s", key[0], key[1])
		}
		table[key] = refuses
	}
	return table, nil
}

/*
================
refuses

Whether a link refuses a summoned transport: the server's answer when the
table has the link, else the client's kind.
================
*/
func (t portalTransport) refuses(source, target string, clientTransport bool) bool {
	if refuses, known := t[[2]string{source, target}]; known {
		return refuses
	}
	return clientTransport
}
