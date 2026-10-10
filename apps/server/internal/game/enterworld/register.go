/*
===========================================================================

register.go - service state and lifecycle integration

===========================================================================
*/
package enterworld

import (
	"encoding/json"
	"strings"

	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/cluster/shard"
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/transport"
)

// Session keys the enter-world bind writes. GO-2/GO-3 filter broadcasts and
// resolve the acting character through these; the division key matches the
// transport package's own BroadcastFunc documentation example.
// EnterWorldBlobVersion versions the OpEnterWorldResult blob.
const EnterWorldBlobVersion = 1

// enterWorldBlob wraps the bootstrap envelope for the 0x0007 blob:
// {"v":1,"bootstrap":<envelope minus packets>}. The packets do NOT ride the
// blob - after the result frame they arrive as ordinary native frames in
// order, which is the whole point of the transport cutover.
/*
================
enterWorldBlob
================
*/
type enterWorldBlob struct {
	V         int             `json:"v"`
	Bootstrap json.RawMessage `json:"bootstrap"`
}

// EnterWorldBlob renders the versioned blob for one bootstrap result.
/*
================
EnterWorldBlob
================
*/
func EnterWorldBlob(result *BootstrapResult) ([]byte, error) {
	envelope, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	// Strip the packets array: the blob is "bootstrap DTO minus packets"
	// (frozen contract, internal/transport/envelope.go OpEnterWorldResult comment).
	var asMap map[string]json.RawMessage
	if err := json.Unmarshal(envelope, &asMap); err != nil {
		return nil, err
	}
	delete(asMap, "packets")
	// Explicit browser vitals use the same clamped values as native entry.
	// Persisted Character uses currentHp/currentMp; forwarding it verbatim left
	// the browser's hp/mp fields absent until a later game-ready refresh.
	if result.Character != nil {
		var character map[string]json.RawMessage
		if err := json.Unmarshal(asMap["character"], &character); err != nil {
			return nil, err
		}
		for key, value := range map[string]int64{"hp": CurrentHP(result.Character), "mp": CurrentMP(result.Character), "maxHp": DerivedMaxHP(result.Character), "maxMp": DerivedMaxMP(result.Character)} {
			character[key], err = json.Marshal(value)
			if err != nil {
				return nil, err
			}
		}
		asMap["character"], err = json.Marshal(character)
		if err != nil {
			return nil, err
		}
	}
	stripped, err := json.Marshal(asMap)
	if err != nil {
		return nil, err
	}
	return json.Marshal(enterWorldBlob{V: EnterWorldBlobVersion, Bootstrap: stripped})
}

// browserEntryPayload is shared by first entry and resident-world replacement.
// The browser consumes this projection, not the native character chunk schema.
/*
================
browserEntryPayload
================
*/
func browserEntryPayload(result *BootstrapResult, references *BrowserReferences) ([]byte, error) {
	var blob []byte
	var err error
	if references != nil {
		blob, err = referenceEnterWorldBlob(result, references)
	} else {
		blob, err = EnterWorldBlob(result)
	}
	if err != nil {
		return nil, err
	}
	return transport.EncodeEnterWorldResult(transport.EnterWorldResult{OK: true, Blob: blob}), nil
}

// EnterWorldOutcome is HandleEnterWorld's transport-agnostic answer: the
// encoded 0x0007 payload plus the native frames to push after it, in order.
/*
================
EnterWorldOutcome
================
*/
type EnterWorldOutcome struct {
	OK bool
	// DivisionID/CharacterName are the resolved session identity bind
	// (only meaningful when OK).
	DivisionID    string
	CharacterName string
	// ResultPayload is the encoded OpEnterWorldResult payload.
	ResultPayload []byte
	// Frames are the bootstrap packets as native frames.
	Frames []Packet
	// Result is the full envelope, for logging/tests.
	Result *BootstrapResult
}

// HandleEnterWorld runs the enter-world bind over a decoded-or-raw
// OpEnterWorld payload. Pure with respect to the transport: the Hub glue in
// RegisterEnterWorld only moves the outcome onto the session.
/*
================
HandleEnterWorld
================
*/
func HandleEnterWorld(deps *Deps, payload []byte) EnterWorldOutcome {
	return handleEnterWorldWithDiagnostics(deps, payload, "")
}

/*
================
handleEnterWorldWithDiagnostics
================
*/
func handleEnterWorldWithDiagnostics(deps *Deps, payload []byte, session string) EnterWorldOutcome {
	decoded, err := transport.DecodeEnterWorld(payload)
	if err != nil {
		failure := Failure(nativeErrorInvalidRequest, "malformedEnterWorld")
		return enterWorldFailureOutcome(failure)
	}
	result := Build(deps, BootstrapRequest{
		DivisionID:    decoded.Division,
		CharacterName: decoded.CharName,
	})
	if result.NativeResult != 1 {
		return enterWorldFailureOutcome(result)
	}
	result.DiagnosticSessionID = session
	resultPayload, err := browserEntryPayload(result, deps.BrowserReferences)
	if err != nil {
		failure := Failure(nativeErrorInvalidRequest, "blobEncodeFailed")
		return enterWorldFailureOutcome(failure)
	}
	deps.adoptEncodedInventory(result)
	characterName := ""
	if result.Character != nil {
		characterName = result.Character.Name
	}
	// The community seeds (empty friend roster / letter list) ride AFTER
	// the bootstrap sequence, on the transport outcome only: Build's
	// Packets are a frozen fixture contract, and the client's community
	// folds (0x3769 sub_75ac30 / 0xB3CD sub_75cef0) dispatch fine once
	// the mission packet owner is live, which the preceding entered
	// flush guarantees. A fresh slice - appending onto result.Packets
	// would alias the envelope's backing array.
	frames := result.Packets
	var seeds []Packet
	if deps.CommunitySeedFramesFor != nil {
		// Division-aware by necessity: the letter seed reads the memos
		// table, which is keyed by division + character id.
		seeds = deps.CommunitySeedFramesFor(result.DivisionID, result.Character)
	}
	if len(seeds) > 0 {
		combined := make([]Packet, 0, len(frames)+len(seeds))
		combined = append(combined, frames...)
		combined = append(combined, seeds...)
		frames = combined
	}
	return EnterWorldOutcome{
		OK:            true,
		DivisionID:    result.DivisionID,
		CharacterName: characterName,
		ResultPayload: resultPayload,
		Frames:        frames,
		Result:        result,
	}
}

/*
================
enterWorldFailureOutcome
================
*/
func enterWorldFailureOutcome(result *BootstrapResult) EnterWorldOutcome {
	blob, err := EnterWorldBlob(result)
	if err != nil {
		blob = nil
	}
	return EnterWorldOutcome{
		OK: false,
		ResultPayload: transport.EncodeEnterWorldResult(transport.EnterWorldResult{
			OK:              false,
			NativeErrorCode: uint32(result.NativeErrorCode),
			Blob:            blob,
		}),
		Result: result,
	}
}

// packetBytes re-arms a JSON-shaped packet payload for the wire.
/*
================
packetBytes
================
*/
func packetBytes(p Packet) []byte {
	out := make([]byte, len(p.Payload))
	for index, value := range p.Payload {
		out[index] = byte(value)
	}
	return out
}

// RegisterEnterWorld registers the OpEnterWorld (0x0006) handler: decode,
// bootstrap, answer OpEnterWorldResult (0x0007), bind the session identity,
// then push the bootstrap packets as native frames in order. All frames ride
// the RELIABLE path (bootstrap opcodes are not in the datagram allowlist).
// Mission-tick visibility is deliberately NOT installed here: the client has
// not announced 0x3012 game-ready yet, so a peer 0x30D7 could overtake scene
// admission and then be suppressed forever by the ticker's shown set.
/*
================
RegisterEnterWorld
================
*/
func RegisterEnterWorld(hub *transport.Hub, deps *Deps) {
	hub.Handle(transport.OpEnterWorld, func(s *transport.Session, _ uint16, payload []byte) {
		admission := *deps
		// Acquire before invalidating the old scene. A committed pickup must
		// publish before this replacement snapshot, including entry refills.
		if deps.LockPublication != nil {
			if request, err := transport.DecodeEnterWorld(payload); err == nil {
				division := resolveBootstrapDivision(deps, request.Division)
				admission.ResolveDivisionID = func(string) string { return division }
				if division != "" {
					unlock := deps.LockPublication(division)
					defer unlock()
				}
			}
		}
		s.BeginSceneAdmission()
		var claimedDivision, claimedName string
		published := false
		defer func() {
			if !published && claimedName != "" && deps.RetireCharacterSession != nil {
				deps.RetireCharacterSession(claimedDivision, claimedName, s.ID)
			}
		}()
		if deps.AdmitCharacterSession != nil {
			admission.RestoreEntryEffects = nil
			admission.PrepareEntry = func(division, name string) error {
				// The claim remains provisional until the complete bootstrap can
				// be published. The deferred owner-specific teardown covers a
				// close before binding as well as projection/delivery failure.
				if err := deps.AdmitCharacterSession(division, name, s.ID); err != nil {
					return err
				}
				claimedDivision, claimedName = division, name
				return nil
			}
		}
		outcome := handleEnterWorldWithDiagnostics(&admission, payload, s.DiagnosticSessionID())
		if outcome.OK {
			// Bind identity BEFORE the result frame so any handler racing
			// on another frame already sees the bound character.
			s.BindCharacter(outcome.DivisionID, outcome.CharacterName, ObjectIDForCharacter(outcome.Result.Character))
		}
		// EnterWorld is one ordered admission transaction. Publishing it as
		// one batch prevents a large legal object list from racing the socket
		// writer against the transport queue cap, and prevents unrelated
		// producers from interleaving inside the native bootstrap brackets.
		frames := make([]transport.Frame, 1, 1+len(outcome.Frames))
		frames[0] = transport.Frame{
			Opcode:  transport.OpEnterWorldResult,
			Payload: outcome.ResultPayload,
		}
		for _, frame := range outcome.Frames {
			frames = append(frames, transport.Frame{
				Scope:   transport.ScopeChanges(frame.Scope),
				Opcode:  frame.NativeOpcode,
				Payload: packetBytes(frame),
			})
		}
		send := s.SendBatch
		if outcome.OK {
			// A resumed transport also receives a replacement bootstrap. It must
			// invalidate the previous scene's visibility just like town return.
			send = s.SendSceneReset
		}
		if err := send(frames); err != nil {
			log.WithFields(log.Fields{
				"character": outcome.CharacterName,
				"division":  outcome.DivisionID,
				"frames":    len(frames),
				"error":     err,
			}).Warn("bootstrap: atomic admission delivery failed")
		} else {
			published = outcome.OK
		}
	})
}

// SessionCharacter resolves the session's bound character through the
// enter-world identity keys. GO-2/GO-3 handlers should use this rather than
// trusting client-supplied names on later frames.
/*
================
SessionCharacter
================
*/
func SessionCharacter(source CharacterSource, s *transport.Session) (*Character, string, bool) {
	divisionID, characterName, ok := s.CharacterBinding()
	if !ok || source == nil {
		return nil, "", false
	}
	for _, candidate := range source.CharactersForDivision(divisionID) {
		if candidate != nil && candidate.Name == characterName {
			return candidate, divisionID, true
		}
	}
	return nil, "", false
}

// HandleGameReady ports buildGameReadyRuntimePush's packet half: the 0x31AD
// game clock, 0x343C maximum/base stats, and 0x33A6 current vitals refresh for
// the bound character. No bound character means no packets (the Node side
// answers an empty packet list).
/*
================
HandleGameReady
================
*/
func HandleGameReady(character *Character, stats wire.BaseStats) []Packet {
	if character == nil {
		return nil
	}
	frames := []Packet{
		NewPacket(OpcodeGameTime, BuildGameTimePayload()),
		NewPacket(wire.OpBaseStats, BuildLoginStatBlock(character, stats)),
		NewPacket(OpcodeVitalsUpdate, BuildVitalsRefreshPayload(character)),
	}
	if character.NativeBodyStatus != 0 {
		frames = append(frames, NewPacket(wire.OpObjectStateRefresh, (wire.ObjectStateRefresh{Gid: ObjectIDForCharacter(character), StateType: wire.StateChannelBody, Value: character.NativeBodyStatus}).Encode()))
	}
	// Current HP rides the entered corpus, but native life state does not.
	// Game-ready runs after 0x31DB committed the local CICPlayer, so replay a
	// persisted corpse through the same 0x3122 LIFE-dead transition as a live
	// fatal hit. Otherwise the actor looks alive while every combat authority
	// correctly refuses it.
	if !CharacterAlive(character) {
		frames = append(frames, NewPacket(
			wire.OpObjectStateRefresh,
			wire.ObjectStateRefresh{
				Gid: ObjectIDForCharacter(character), StateType: wire.StateChannelLife, Value: wire.LifeStateDead,
			}.Encode(),
		))
	}
	return frames
}

// OpcodeGameReady is the C->S game-ready trigger (agentGameReadyOpcode).
const OpcodeGameReady uint16 = 0x3012

// RegisterGameReady registers the 0x3012 handler: the client announces the
// scene is live, receives the game-time + vitals runtime push, and only then
// becomes visible to the simulation ticker. Send() is a FIFO reliable enqueue;
// installing WorldBound after those enqueues guarantees every later peer
// spawn is ordered behind the complete enter-world and game-ready bursts.
/*
================
RegisterGameReady
================
*/
func RegisterGameReady(hub *transport.Hub, deps *Deps) {
	hub.Handle(OpcodeGameReady, func(s *transport.Session, _ uint16, _ []byte) {
		reentry := s.FinishSceneReentry() && s.WorldReady()
		if !reentry && !s.TryMarkWorldReady() {
			return
		}
		character, divisionID, ok := SessionCharacter(deps.Characters, s)
		if !ok {
			// BindCharacter is the owner of admission identity. A vanished
			// authority record cannot remain admitted to an empty world.
			s.ClearGameplayContext()
			return
		}
		snapshot := readCharacterSnapshot(deps, divisionID, character)
		// Production composition requires PlayerBaseStats through Validate.
		// Detached package tests may register the bootstrap lane alone; they
		// retain the historical zero projection while exercising unrelated
		// wire lifecycles. A wired owner failing is different: that is an
		// invalid authoritative snapshot and must fail closed.
		stats := wire.BaseStats{}
		if deps.PlayerBaseStats != nil {
			var err error
			stats, err = deps.PlayerBaseStats(snapshot)
			if err != nil {
				log.Warnf("bootstrap: game-ready refused for %q - %v", character.Name, err)
				s.ClearGameplayContext()
				return
			}
		}
		for _, frame := range HandleGameReady(snapshot, stats) {
			if err := s.Send(frame.NativeOpcode, packetBytes(frame)); err != nil {
				return
			}
		}
		if deps.SceneReferenceFrames != nil {
			for _, frame := range deps.SceneReferenceFrames() {
				if err := s.Send(frame.Opcode, frame.Payload); err != nil {
					return
				}
			}
		}
		// Every game-ready that ends a loading scene (first entry, travel,
		// a resumed transport's replacement bootstrap) settles the objects
		// the bootstrap published against the live world.
		if deps.ReconcileSceneObjects != nil {
			deps.ReconcileSceneObjects(s, divisionID, snapshot)
		}
		// The hook gets the STORE record, not the detached snapshot: gameplay
		// lanes share authoritative pointer identity. TryMarkWorldReady makes
		// this lifecycle edge one-shot; reconnect resumes the same session and
		// must not replay world-entry side effects.
		if !reentry && deps.OnWorldBound != nil {
			deps.OnWorldBound(s, divisionID, character)
		}
	})
}

// Register wires every bootstrap-lane handler onto the hub.
/*
================
Register
================
*/
func Register(hub *transport.Hub, deps *Deps) {
	RegisterEnterWorld(hub, deps)
	RegisterGameReady(hub, deps)
	RegisterQuickSlotBindings(hub, deps)
	RegisterEventGuideAck(hub, deps)
	RegisterStallNetworkLeave(hub, deps)
}

// DevResolveDivisionID is the dev twin of the Node divisionIdFor title-config
// half: a request division listed in the shard catalog (when loaded) or
// present in the authority store wins; anything else (the audit scripts'
// numeric 0, an empty value) lands on the catalog default shard, or
// domain.DefaultDivisionID when no catalog is available.
/*
================
DevResolveDivisionID
================
*/
func DevResolveDivisionID(
	source CharacterSource,
	allowedShardIDs []string,
	defaultShardID string,
) func(string) string {
	if defaultShardID == "" {
		defaultShardID = DefaultDivisionID
	}
	allowed := make(map[string]struct{}, len(allowedShardIDs))
	for _, shardID := range allowedShardIDs {
		shardID = strings.TrimSpace(shardID)
		if shardID != "" {
			allowed[shardID] = struct{}{}
		}
	}
	return func(requestDivisionID string) string {
		if requestDivisionID != "" {
			if _, ok := allowed[requestDivisionID]; ok {
				return requestDivisionID
			}
			if source != nil && len(source.CharactersForDivision(requestDivisionID)) > 0 {
				return requestDivisionID
			}
		}
		return defaultShardID
	}
}

/*
================
devShardPolicy
================
*/
type devShardPolicy struct {
	allowedIDs []string
	defaultID  string
}

/*
================
devShardPolicyFromCatalog

Loads allowed shard ids from config/shards.json when the process can read it.
An missing catalog is non-fatal: callers fall back to store-known divisions
and domain.DefaultDivisionID.
================
*/
func devShardPolicyFromCatalog() devShardPolicy {
	catalog, path, err := shard.LoadFromEnv()
	if err != nil {
		log.Warnf(
			"bootstrap: shard catalog unavailable (%s: %v); division resolve uses store data and %q default",
			path,
			err,
			domain.DefaultDivisionID,
		)
		return devShardPolicy{defaultID: domain.DefaultDivisionID}
	}
	return devShardPolicy{
		allowedIDs: catalog.IDs(),
		defaultID:  catalog.Default().ID,
	}
}

/*
================
DevResolveDivisionIDFromCatalog

Dev/test wiring helper: builds DevResolveDivisionID from the live shard
catalog instead of a hardcoded division list.
================
*/
func DevResolveDivisionIDFromCatalog(source CharacterSource) func(string) string {
	policy := devShardPolicyFromCatalog()
	return DevResolveDivisionID(source, policy.allowedIDs, policy.defaultID)
}
