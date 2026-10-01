/*
===========================================================================

register.go - fortress state publication and session admission ordering

One lock orders war transitions with entering players' complete state seeds.
The optional seed emitter is not the persistent fortress gameplay authority.

===========================================================================
*/
package siege

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"

	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/transport"
)

/*
================
warPublisher

The transport hub owns delivery; this boundary also permits wire-level tests.
================
*/
type warPublisher interface {
	Broadcast(uint16, []byte)
}

/*
================
Runtime

Owns the optional fortress-state emitter, including the 3887 state list and
341E alliance seed. Request authority and persistent scheduling remain in
the expanded implementation scope. Nil runtimes safely omit publication.
================
*/
type Runtime struct {
	hub  warPublisher
	seed []WarRow
	mu   sync.RWMutex
	// warActive mirrors whether the seeded war list should arrive
	// already active: the seed frame carries WarFlagSiegeWar in its
	// globalFlags byte (the @0x76cae7 OR) instead of a separate
	// SubtypeWarBegin frame.
	warActive bool
	// fortressListID rides the subtype-0 tail into the client's
	// relation block +0x14 (@0x76cb6d).
	fortressListID uint32
	// Registered GUILD IDs from subtype 0x10; never infer these from war IDs.
	guildIDs []uint32
	// allies is the optional 0x341E siege-relation seed (the relation
	// block +0x94 map, the GetStatus 0xcb ally leg). Empty = the frame
	// is not sent.
	allies []AllianceRow
}

// SeedEnvVar enables the lane: unset (the default) keeps it fully inert.
// "1" seeds the built-in POLICY dev fixture (one war row - the minimum
// for a war to be activatable); any other value is a comma-separated
// list of id:name rows, e.g. "1:JanganWar,3:HotanWar".
const SeedEnvVar = "SRO_FORTRESS_WAR_SEED"

// ActiveEnvVar ("1") makes the seed arrive with WarFlagSiegeWar already
// set in globalFlags, so the war is active from enter-world without a
// separate BroadcastWarBegin. Ignored while SeedEnvVar is unset.
const ActiveEnvVar = "SRO_FORTRESS_WAR_ACTIVE"

// GuildsEnvVar supplies registered guild IDs. Unset leaves the registry empty.
const GuildsEnvVar = "SRO_FORTRESS_WAR_GUILDS"

// AlliesEnvVar seeds the 0x341E siege-relation list: comma-separated
// id:name rows (id = the ally GUILD id GetStatus exact-finds for the
// 0xcb leg; name = the relation row's display name). Unset = no 0x341E
// frame. Ignored while SeedEnvVar is unset.
const AlliesEnvVar = "SRO_FORTRESS_WAR_ALLIES"

// devSeedFixture is the built-in POLICY dev fixture (fixture values,
// not retail data): one war row with zero stats and no optional u32s
// (their meanings are unproven - nothing is invented), fortress list id
// = the war id.
/*
================
devSeedFixture
================
*/
func devSeedFixture() []WarRow {
	return []WarRow{{WarID: 1, Name: "FORTRESS_WAR_DEV"}}
}

// NewRuntimeFromEnv builds the lane from the environment: nil (inert)
// unless SeedEnvVar is set. A malformed row spec refuses loudly at boot
// (the deps.Validate posture: a half-wired lane is not diagnosable).
/*
================
NewRuntimeFromEnv
================
*/
func NewRuntimeFromEnv(hub *transport.Hub) (*Runtime, error) {
	spec := strings.TrimSpace(os.Getenv(SeedEnvVar))
	if spec == "" {
		return nil, nil
	}
	seed := devSeedFixture()
	if spec != "1" {
		parsed, err := parseSeedSpec(spec)
		if err != nil {
			return nil, fmt.Errorf("siege: %s: %w", SeedEnvVar, err)
		}
		seed = parsed
	}
	rt := &Runtime{
		hub:            hub,
		seed:           seed,
		warActive:      os.Getenv(ActiveEnvVar) == "1",
		fortressListID: seed[0].WarID,
	}
	if guildSpec := strings.TrimSpace(os.Getenv(GuildsEnvVar)); guildSpec != "" {
		parsed, err := parseGuildSpec(guildSpec)
		if err != nil {
			return nil, fmt.Errorf("siege: %s: %w", GuildsEnvVar, err)
		}
		rt.guildIDs = parsed
	}
	if allySpec := strings.TrimSpace(os.Getenv(AlliesEnvVar)); allySpec != "" {
		parsed, err := parseAllySpec(allySpec)
		if err != nil {
			return nil, fmt.Errorf("siege: %s: %w", AlliesEnvVar, err)
		}
		rt.allies = parsed
	}
	log.Infof("siege: fortress-war seed enabled (%d war(s), active=%v, %d registered guild(s), %d ally row(s)) - 0x3887 rides every enter-world",
		len(seed), rt.warActive, len(rt.guildIDs), len(rt.allies))
	return rt, nil
}

// parseGuildSpec parses the comma-separated u32 guild-id list.
// Id 0 is refused at boot: the client-side insert rejects it silently
// (sub_829580 @0x829588), so shipping one is always a config mistake.
/*
================
parseGuildSpec
================
*/
func parseGuildSpec(spec string) ([]uint32, error) {
	var ids []uint32
	for _, part := range strings.Split(spec, ",") {
		id, err := strconv.ParseUint(strings.TrimSpace(part), 10, 32)
		if err != nil {
			return nil, fmt.Errorf("id %q: %w", part, err)
		}
		if id == 0 {
			return nil, fmt.Errorf("id %q: the client rejects guild id 0", part)
		}
		ids = append(ids, uint32(id))
	}
	return ids, nil
}

// parseAllySpec parses the comma-separated id:name ally rows. The
// unnamed row fields (flag/masterName/refObjId/byte44) stay zero - their
// semantics are unproven and nothing is invented.
/*
================
parseAllySpec
================
*/
func parseAllySpec(spec string) ([]AllianceRow, error) {
	var rows []AllianceRow
	for _, part := range strings.Split(spec, ",") {
		idText, name, found := strings.Cut(strings.TrimSpace(part), ":")
		if !found || name == "" {
			return nil, fmt.Errorf("row %q: want id:name", part)
		}
		id, err := strconv.ParseUint(idText, 10, 32)
		if err != nil {
			return nil, fmt.Errorf("row %q: %w", part, err)
		}
		rows = append(rows, AllianceRow{ID: uint32(id), Name: name})
	}
	return rows, nil
}

// parseSeedSpec parses the comma-separated id:name row list.
/*
================
parseSeedSpec
================
*/
func parseSeedSpec(spec string) ([]WarRow, error) {
	var rows []WarRow
	for _, part := range strings.Split(spec, ",") {
		idText, name, found := strings.Cut(strings.TrimSpace(part), ":")
		if !found || name == "" {
			return nil, fmt.Errorf("row %q: want id:name", part)
		}
		id, err := strconv.ParseUint(idText, 10, 32)
		if err != nil {
			return nil, fmt.Errorf("row %q: %w", part, err)
		}
		rows = append(rows, WarRow{WarID: uint32(id), Name: name})
	}
	return rows, nil
}

// seedGlobalFlags derives the subtype-0 trailing flags byte.
/*
================
seedGlobalFlags
================
*/
func (rt *Runtime) seedGlobalFlags() uint8 {
	// WorldBound holds the read lock through the entire seed publication.
	if rt.warActive {
		return WarFlagSiegeWar
	}
	return 0
}

// WorldBound pushes the fortress-war seed frames to ONE entering session
// (called from the enter-world winner tail, after the bootstrap frames -
// the community seed-frame posture): the subtype-0 war list, the
// subtype-0x10 war-guild registry (clear-and-replace of the client's
// FortressMgr+0x2f4 set - without it GetStatus can never return
// 0xc9/0xca), and - when configured - the 0x341E siege-relation list
// (the +0x94 map, the 0xcb ally leg). Nil-safe: the inert default sends
// nothing and existing enter-world behavior is untouched.
/*
================
WorldBound
================
*/
func (rt *Runtime) WorldBound(s *transport.Session) {
	if rt == nil || s == nil {
		return
	}
	rt.mu.RLock()
	defer rt.mu.RUnlock()
	_ = s.Send(OpFortressWarState, EncodeWarList3887(rt.seed, rt.seedGlobalFlags(), rt.fortressListID))
	// The echo dword is parsed then discarded by the client (@0x76e3e5);
	// 0 matches the other subtypes' inert "mgrPtr" convention.
	_ = s.Send(OpFortressWarState, EncodeWarGuildRegistry3887(0, rt.guildIDs))
	if len(rt.allies) > 0 {
		// The unnamed header dwords stay 0 (semantics unproven); the
		// master guild id too - the ally rows alone feed the 0xcb leg.
		_ = s.Send(OpSiegeRelationList, EncodeSiegeRelationList341E(0, 0, 0, rt.allies))
	}
}

// BroadcastWarBegin pushes the subtype-2 WAR_BEGIN to every session and
// latches the active state for later joiners' seeds. The war state is
// global on the client (sub_7e2100 walks the WHOLE map), so a hub
// broadcast is the faithful fan-out.
/*
================
BroadcastWarBegin
================
*/
func (rt *Runtime) BroadcastWarBegin() {
	rt.setWarActive(true)
}

// BroadcastWarEnd pushes the subtype-6 WAR_END to every session and
// clears the active latch.
/*
================
BroadcastWarEnd
================
*/
func (rt *Runtime) BroadcastWarEnd() {
	rt.setWarActive(false)
}

/*
================
setWarActive

Client 7E2148 XORs a disabled flag. Repeated end messages would re-enable
war, so only real transitions may reach the wire. Hold the same lock through
publication and entry seeding to prevent an older seed following a newer end.
================
*/
func (rt *Runtime) setWarActive(active bool) {
	if rt == nil {
		return
	}
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if rt.warActive == active {
		return
	}
	rt.warActive = active
	payload := EncodeWarEnd3887()
	if active {
		payload = EncodeWarBegin3887()
	}
	rt.hub.Broadcast(OpFortressWarState, payload)
}
