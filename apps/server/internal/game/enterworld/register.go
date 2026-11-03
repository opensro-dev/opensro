/*
===========================================================================

register.go - service state and lifecycle integration

===========================================================================
*/
package enterworld

import (
	"time"

	"strings"

	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/cluster/shard"
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/platform/jsonscratch"
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
	V         int `json:"v"`
	Bootstrap any `json:"bootstrap"`
}

// EnterWorldBlob renders the versioned blob for one bootstrap result.
/*
================
EnterWorldBlob
================
*/
func EnterWorldBlob(result *BootstrapResult) ([]byte, error) {
	return marshalBlob(plainBlobValue(result))
}

/*
================
plainBlobValue

The plain blob's one shape, shared by EnterWorldBlob and the borrowed
per-entry path (browserEntryPayload).
================
*/
func plainBlobValue(result *BootstrapResult) enterWorldBlob {
	return enterWorldBlob{
		V:         EnterWorldBlobVersion,
		Bootstrap: result.blobView(),
	}
}

// browserEntryPayload is shared by first entry and resident-world replacement.
// The browser consumes this projection, not the native character chunk schema.
/*
================
browserEntryPayload
================
*/
func browserEntryPayload(result *BootstrapResult, references *BrowserReferences) ([]byte, error) {
	// The blob is borrowed from the pooled scratch and copied exactly
	// once, by EncodeEnterWorldResult into the envelope; the owned
	// encoders above would clone it first for bytes nobody keeps.
	scratch := blobScratchPool.Get().(*jsonscratch.Scratch)
	var blob []byte
	var err error
	if references != nil {
		blob, err = scratch.Marshal(referenceBlobValue(result, references))
	} else {
		blob, err = scratch.Marshal(plainBlobValue(result))
	}
	if err != nil {
		releaseBlobScratch(scratch)
		return nil, err
	}
	payload := transport.EncodeEnterWorldResult(transport.EnterWorldResult{OK: true, Blob: blob})
	releaseBlobScratch(scratch)
	return payload, nil
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
	result, failure, ok := buildEnterWorld(deps, payload)
	if !ok {
		return failure
	}
	return encodeEnterWorld(deps, result, session)
}

/*
================
buildEnterWorld

Decode and Build only: the snapshot the publication gate orders
against tick commits. The encode that follows reads nothing but the
detached result, so the enter handler releases the gate between the
two (RegisterEnterWorld).
================
*/
func buildEnterWorld(deps *Deps, payload []byte) (*BootstrapResult, EnterWorldOutcome, bool) {
	decoded, err := transport.DecodeEnterWorld(payload)
	if err != nil {
		failure := Failure(nativeErrorInvalidRequest, "malformedEnterWorld")
		return nil, enterWorldFailureOutcome(failure), false
	}
	result := Build(deps, BootstrapRequest{
		DivisionID:    decoded.Division,
		CharacterName: decoded.CharName,
	})
	if result.NativeResult != 1 {
		return nil, enterWorldFailureOutcome(result), false
	}
	return result, EnterWorldOutcome{}, true
}

/*
================
encodeEnterWorld

Marshal the detached result, adopt what it presents and assemble the
outcome. Touches no live record but its own doors (the adoption, the
login stamp), so it may run after the publication gate is released.
================
*/
func encodeEnterWorld(deps *Deps, result *BootstrapResult, session string) EnterWorldOutcome {
	result.DiagnosticSessionID = session
	resultPayload, err := deps.entryPayload(result)
	if err != nil {
		failure := Failure(nativeErrorInvalidRequest, "blobEncodeFailed")
		return enterWorldFailureOutcome(failure)
	}
	loggedIn := result.liveOwner
	deps.adoptEncodedInventory(result)
	// Port-only, not native: the community site's player database shows the
	// latest login (Character.LastLoginAtMs). Teleport re-entries do not count.
	if loggedIn != nil {
		at := time.Now()
		if deps.Now != nil {
			at = deps.Now()
		}
		deps.Mutate(loggedIn, "last-login", func() { loggedIn.LastLoginAtMs = at.UnixMilli() })
	}
	characterName := ""
	if result.Character != nil {
		characterName = result.Character.Name
	}
	// The community seeds (empty friend roster / letter list) ride AFTER
	// the bootstrap sequence, on the transport outcome only: Build's
	// Packets are a frozen fixture contract, and the client's community
	// folds (client 0x3769 75AC30 / 0xB3CD 75CEF0) dispatch fine once
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
	copy(out, p.Payload)
	return out
}

// defaultSlowEnterThreshold is the handler duration that earns the
// "bootstrap: slow enter world" phase log when deps.SlowEnterThreshold
// is unset. Completed entries in production finish in well under a
// second; five seconds sits far above the worst legitimate build and
// far below the client's own admission patience, so a firing line
// always names a real wait.
const defaultSlowEnterThreshold = 5 * time.Second

// defaultGateWaitBound is the publication-gate wait that abandons an
// entry when deps.GateWaitBound is unset. A legitimate gate hold is
// another entry's build and publish, sub-second even in the slowest
// measured entries; ten seconds is the client's own admission
// patience, so a wait past it is a wedge, not slowness, and the entry
// is worth more as a log line than as a silently parked handler.
const defaultGateWaitBound = 10 * time.Second

/*
================
acquireEnterGate

Take the division publication gate for one entry, giving up when the
session closes or the bound passes. The gate is taken before scene
admission, so a handler parked on it is invisible to both the
bootstrap watchdog and the slow-entry log; abandoning the entry
unanswered leaves the wait in the log instead, and the client
retries. The deferred taker still releases the gate when it frees,
so an abandoned wait never wedges the queue behind it.
================
*/
func acquireEnterGate(lock func(string, string) func(), division string, done <-chan struct{}, bound time.Duration) (func(), bool) {
	acquired := make(chan func(), 1)
	go func() { acquired <- lock(division, "enterWorld") }()
	timer := time.NewTimer(bound)
	defer timer.Stop()
	select {
	case unlock := <-acquired:
		return unlock, true
	case <-done:
	case <-timer.C:
	}
	go func() { (<-acquired)() }()
	return nil, false
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
		enteredAt := time.Now()
		slowThreshold := deps.SlowEnterThreshold
		if slowThreshold <= 0 {
			slowThreshold = defaultSlowEnterThreshold
		}
		gateBound := deps.GateWaitBound
		if gateBound <= 0 {
			gateBound = defaultGateWaitBound
		}
		var requestName string
		var gateWait time.Duration
		// releaseGate frees the publication gate early (after Build, below)
		// and is a no-op when no gate was taken; the defer is its safety net.
		releaseGate := func() {}
		// Acquire before invalidating the old scene. A committed pickup must
		// publish before this replacement snapshot, including entry refills.
		if deps.LockPublication != nil {
			if request, err := transport.DecodeEnterWorld(payload); err == nil {
				requestName = request.CharName
				division := resolveBootstrapDivision(deps, request.Division)
				admission.ResolveDivisionID = func(string) string { return division }
				if division != "" {
					unlock, ok := acquireEnterGate(deps.LockPublication, division, s.Done(), gateBound)
					gateWait = time.Since(enteredAt)
					if !ok {
						// The client leaving needs no name; a live
						// session abandoned at the gate is the wedge
						// this bound exists to expose.
						select {
						case <-s.Done():
						default:
							log.WithFields(log.Fields{
								"session":   s.DiagnosticSessionID(),
								"character": requestName,
								"division":  division,
								"waited_ms": gateWait.Milliseconds(),
								"bound_ms":  gateBound.Milliseconds(),
							}).Warn("bootstrap: enter world abandoned at the publication gate")
						}
						return
					}
					release := unlock
					releaseGate = func() {
						if release != nil {
							release()
							release = nil
						}
					}
					defer releaseGate()
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
		result, failure, built := buildEnterWorld(&admission, payload)
		if built {
			// Bind identity before the gate opens (#2294): lanes that
			// wake during the encode below must see this scene's
			// not-ready binding, not the previous scene's ready one —
			// a shared pickup's recipient rotation reads exactly that.
			s.BindCharacter(result.DivisionID, result.Character.Name, ObjectIDForCharacter(result.Character))
		}
		// The gate ordered this snapshot against tick commits; Build is
		// the last read of live state. Mark the snapshot built while the
		// gate still serializes producers, then open it: the encode and
		// publish below read only the detached result, so the marshal no
		// longer queues every tick behind it (#2294). Wire order still
		// holds — visibility stays fenced until the bootstrap lands,
		// and work committed from now on parks behind it in the
		// transport until the bootstrap batch queues.
		s.MarkSceneSnapshotBuilt()
		defer s.CloseSceneSnapshotBuilt()
		releaseGate()
		var outcome EnterWorldOutcome
		if !built {
			outcome = failure
		} else {
			outcome = encodeEnterWorld(&admission, result, s.DiagnosticSessionID())
		}
		// EnterWorld publishes as one ordered batch: a large legal object
		// list cannot race the socket writer against the transport queue
		// cap, and unrelated producers cannot interleave inside the
		// native bootstrap brackets. The batch goes out after the
		// publication gate opens (#2294); the transport's visibility
		// fence and receipt park keep this snapshot first on the wire.
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
		publish := func() error { return send(frames) }
		if outcome.OK && deps.PublishEntry != nil {
			character := outcome.Result.Character
			publish = func() error {
				return deps.PublishEntry(s, outcome.DivisionID, character, func() error { return send(frames) })
			}
		}
		if err := publish(); err != nil {
			log.WithFields(log.Fields{
				"character": outcome.CharacterName,
				"division":  outcome.DivisionID,
				"frames":    len(frames),
				"error":     err,
			}).Warn("bootstrap: atomic admission delivery failed")
		} else {
			published = outcome.OK
			// The scene has its answer on the wire (bootstrap or
			// refusal): the transport's unenqueued-window watchdog
			// stands down for this admission.
			s.MarkSceneBootstrapEnqueued()
		}
		// A slow entry names its phases while the trail is warm: the
		// gate wait, the build and the publish all hide behind the same
		// silent loading screen from the player's side, and completed
		// entries are otherwise invisible beyond a histogram bucket.
		if elapsed := time.Since(enteredAt); elapsed >= slowThreshold {
			log.WithFields(log.Fields{
				"session":   s.DiagnosticSessionID(),
				"character": requestName,
				"division":  outcome.DivisionID,
				"gate_ms":   gateWait.Milliseconds(),
				"total_ms":  elapsed.Milliseconds(),
				"ok":        outcome.OK,
			}).Warn("bootstrap: slow enter world")
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

// OpcodeResetClientAck is the client's empty answer to OpcodeResetClient
// (0x3369): the native client sends 0x36DD first, then applies the reset. Native capture shows no server response and no state riding
// on it - a bare acknowledgement the native server ignores
// (research/docs/reverse-engineering/gateway/GatewayProtocol.md).
const OpcodeResetClientAck uint16 = 0x36DD

// ConsentAnswerLength is the body of the native client's only 0x3012
// send: the confirm-box answer {u32, u32, u8} (NetClient 701A50 from
// CGInterface_OnMsgBoxResult, the friend request's prompt type 0x23). The
// port's own client sends 0x3012 empty as game-ready, so the length tells
// them apart.
const ConsentAnswerLength = 9

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
	hub.Handle(OpcodeGameReady, func(s *transport.Session, _ uint16, payload []byte) {
		// A consent answer is not a scene signal: it never reaches the
		// game-ready state machine below.
		if len(payload) == ConsentAnswerLength {
			if deps.FriendConsent != nil && s.WorldReady() {
				deps.FriendConsent(s, payload)
			}
			return
		}
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

/*
================
RegisterResetClientAck

Takes the 0x36DD reset acknowledgement and drops it, the native server's
own posture. Registering the deliberate ignore matters: an unregistered
ack lands in the unhandled-opcode counter once per scene reset (#2147),
once per world entry and teleport, which is exactly the noise that hid
opcodes nobody had explained yet.
================
*/
func RegisterResetClientAck(hub *transport.Hub) {
	hub.Handle(OpcodeResetClientAck, func(s *transport.Session, _ uint16, _ []byte) {
		log.Debugf("bootstrap: reset-client ack from session %d ignored", s.ID)
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
	RegisterResetClientAck(hub)
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
