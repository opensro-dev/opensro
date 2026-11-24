// Package shard owns the process-wide shard catalog.
//
// A shard is one isolated character roster and game world. Accounts are
// global, but every character, session, social record, and world mutation is
// scoped by a shard ID selected at title login.
package shard

import (
	"fmt"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	MaxIDBytes   = 64
	MaxNameBytes = 64
	MaxCapacity  = 1_000_000
)

var idPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// Definition is one immutable title-list shard and its admission policy.
type Definition struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	NativeServerID uint16 `json:"nativeServerId"`
	NativeFarmID   uint16 `json:"nativeFarmId"`
	Capacity       int    `json:"capacity"`
	Default        bool   `json:"default"`
	Test           bool   `json:"test"`
	Enabled        bool   `json:"enabled"`
	// ControlURL is the Agent-to-GameWorld authority endpoint. It is never
	// returned to public clients.
	ControlURL string `json:"controlUrl"`
	// TransportURL is the GameWorld's own game transport origin: its default
	// bind address, the upstream a web edge routes to, and what clients dial
	// when no public route is configured.
	TransportURL string `json:"transportUrl"`
	// PublicTransportURL, when set, is advertised to clients instead of
	// TransportURL: either a route such as "/shards/<id>" that the web edge
	// serving the client proxies to TransportURL, so every client origin
	// works unchanged, or the absolute URL of a public transport host.
	PublicTransportURL string `json:"publicTransportUrl,omitempty"`
}

// AdvertisedTransportURL is the transport reference returned to clients. They
// resolve it against the Agent URL they called, like any HTTP reference.
func (definition Definition) AdvertisedTransportURL() string {
	if definition.PublicTransportURL != "" {
		return definition.PublicTransportURL
	}
	return definition.TransportURL
}

// TransportEdgeRoute reports whether AdvertisedTransportURL is an edge route
// ("/shards/<id>") rather than a transport host. The web edge serving the
// client proxies a route over HTTP and carries no QUIC, so clients dial the
// socket alone; a host is the GameWorld's own and offers WebTransport. Sent
// beside the URL so no client infers the form from the string.
func (definition Definition) TransportEdgeRoute() bool {
	return isEdgeRoute(definition.AdvertisedTransportURL())
}

// isEdgeRoute is the one test of the advertised form: a route is a path,
// a host is an absolute URL (validatePublicTransport admits nothing else).
func isEdgeRoute(raw string) bool {
	return strings.HasPrefix(raw, "/")
}

// Catalog is the single source of truth for shards served by this process.
// It is immutable after construction and safe for concurrent readers.
type Catalog struct {
	ordered      []Definition
	byID         map[string]Definition
	defaultShard Definition
}

// NewCatalog validates definitions and freezes them into stable native order.
func NewCatalog(definitions []Definition) (*Catalog, error) {
	if len(definitions) == 0 {
		return nil, fmt.Errorf("shard catalog is empty")
	}

	ordered := append([]Definition(nil), definitions...)
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].NativeServerID != ordered[j].NativeServerID {
			return ordered[i].NativeServerID < ordered[j].NativeServerID
		}
		return ordered[i].ID < ordered[j].ID
	})

	byID := make(map[string]Definition, len(ordered))
	nativeIDs := make(map[uint16]string, len(ordered))
	publicRoutes := make(map[string]string, len(ordered))
	var defaultShard Definition
	for _, definition := range ordered {
		if err := validateDefinition(definition); err != nil {
			return nil, err
		}
		if _, exists := byID[definition.ID]; exists {
			return nil, fmt.Errorf("duplicate shard id %q", definition.ID)
		}
		if route := definition.PublicTransportURL; route != "" {
			if prior, exists := publicRoutes[route]; exists {
				return nil, fmt.Errorf("shards %q and %q share publicTransportUrl %q", prior, definition.ID, route)
			}
			publicRoutes[route] = definition.ID
		}
		if prior, exists := nativeIDs[definition.NativeServerID]; exists {
			return nil, fmt.Errorf(
				"shards %q and %q share nativeServerId %d",
				prior,
				definition.ID,
				definition.NativeServerID,
			)
		}
		if definition.Default {
			if defaultShard.ID != "" {
				return nil, fmt.Errorf(
					"shards %q and %q are both default",
					defaultShard.ID,
					definition.ID,
				)
			}
			defaultShard = definition
		}
		byID[definition.ID] = definition
		nativeIDs[definition.NativeServerID] = definition.ID
	}
	if defaultShard.ID == "" {
		return nil, fmt.Errorf("shard catalog requires exactly one default shard")
	}

	return &Catalog{
		ordered:      ordered,
		byID:         byID,
		defaultShard: defaultShard,
	}, nil
}

func validateDefinition(definition Definition) error {
	if definition.ID == "" ||
		len(definition.ID) > MaxIDBytes ||
		!idPattern.MatchString(definition.ID) {
		return fmt.Errorf(
			"shard id %q must be 1..%d lowercase ASCII letters, digits, or hyphens",
			definition.ID,
			MaxIDBytes,
		)
	}
	if definition.Name == "" ||
		strings.TrimSpace(definition.Name) != definition.Name ||
		len(definition.Name) > MaxNameBytes ||
		!utf8.ValidString(definition.Name) {
		return fmt.Errorf(
			"shard %q name must be unpadded valid UTF-8 of 1..%d bytes",
			definition.ID,
			MaxNameBytes,
		)
	}
	for _, r := range definition.Name {
		if unicode.IsControl(r) {
			return fmt.Errorf("shard %q name contains control text", definition.ID)
		}
	}
	if definition.NativeServerID == 0 {
		return fmt.Errorf("shard %q nativeServerId must be nonzero", definition.ID)
	}
	if definition.Capacity < 1 || definition.Capacity > MaxCapacity {
		return fmt.Errorf(
			"shard %q capacity %d is outside 1..%d",
			definition.ID,
			definition.Capacity,
			MaxCapacity,
		)
	}
	if err := validateEndpoint(definition.ID, "controlUrl", definition.ControlURL); err != nil {
		return err
	}
	if err := validateEndpoint(definition.ID, "transportUrl", definition.TransportURL); err != nil {
		return err
	}
	if definition.PublicTransportURL != "" {
		return validatePublicTransport(definition.ID, definition.PublicTransportURL)
	}
	return nil
}

// validatePublicTransport admits an absolute clean URL or an edge route: a
// canonical absolute path below the site root, never protocol-relative.
func validatePublicTransport(shardID, raw string) error {
	if !isEdgeRoute(raw) {
		return validateEndpoint(shardID, "publicTransportUrl", raw)
	}
	route, err := url.Parse(raw)
	if err != nil || strings.HasPrefix(raw, "//") || raw == "/" || route.Host != "" ||
		route.RawQuery != "" || route.Fragment != "" ||
		path.Clean(raw) != raw || route.EscapedPath() != raw {
		return fmt.Errorf("shard %q publicTransportUrl %q is not a clean absolute path", shardID, raw)
	}
	return nil
}

func validateEndpoint(shardID, field, raw string) error {
	endpoint, err := url.Parse(raw)
	if err != nil || endpoint.Scheme == "" || endpoint.Host == "" ||
		endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" {
		return fmt.Errorf("shard %q %s %q is not an absolute clean URL", shardID, field, raw)
	}
	if endpoint.Scheme != "http" && endpoint.Scheme != "https" {
		return fmt.Errorf("shard %q %s scheme must be http or https", shardID, field)
	}
	return nil
}

// Resolve returns the requested shard. An empty request selects the catalog's
// explicit default; unknown IDs never fall back.
func (catalog *Catalog) Resolve(requested string) (Definition, bool) {
	if catalog == nil {
		return Definition{}, false
	}
	if requested == "" {
		return catalog.defaultShard, true
	}
	definition, ok := catalog.byID[requested]
	return definition, ok
}

// Default returns the explicitly configured default shard.
func (catalog *Catalog) Default() Definition {
	if catalog == nil {
		return Definition{}
	}
	return catalog.defaultShard
}

// Definitions returns a detached stable-order catalog snapshot.
func (catalog *Catalog) Definitions() []Definition {
	if catalog == nil {
		return nil
	}
	return append([]Definition(nil), catalog.ordered...)
}

// IDs returns every configured shard ID in stable native order.
func (catalog *Catalog) IDs() []string {
	definitions := catalog.Definitions()
	ids := make([]string, 0, len(definitions))
	for _, definition := range definitions {
		ids = append(ids, definition.ID)
	}
	return ids
}
