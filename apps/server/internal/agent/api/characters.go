package agentapi

import (
	"errors"
	"net/http"
	"opensro.online/server/internal/releaseprotocol"
	"strings"

	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/data/store"
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/domain/charactervitals"
	"opensro.online/server/internal/security/auth"
)

// CharacterRosterContractVersion is the character-list wire contract, owned
// by the release protocol so it cannot change without a new one: 2 carries
// worn items and avatars as (RefItemID, plus).
const CharacterRosterContractVersion = releaseprotocol.RosterContract

// CharacterItem is one worn or avatar item: native (RefItemID, plus).
type CharacterItem struct {
	RefObjID uint32 `json:"refObjId"`
	Plus     int64  `json:"plus"`
}

// CharacterVisualLoadout is the server-owned render contract exposed to the
// account roster. It mirrors the native character-list record
// (SCharacterInfo_ReadFromPacket): the model, the worn items and the avatar
// items. Browser asset URLs are deliberately absent; the client resolves
// every attachment from its item catalog, as it does in the world.
type CharacterVisualLoadout struct {
	ModelCodename    string          `json:"modelCodename"`
	Items            []CharacterItem `json:"items"`
	Avatars          []CharacterItem `json:"avatars"`
	AnimationSetName string          `json:"animationSetName"`
	HeightScale      float64         `json:"heightScale"`
	VolumeScale      float64         `json:"volumeScale"`
}

// CharacterPresentation is the canonical account-roster projection. Raw
// creation indices remain persistence provenance; every UI-visible identity
// comes from this single projection.
type CharacterPresentation struct {
	RaceIndex         int64
	Gender            int64
	ExperiencePercent *float64
	VisualLoadout     CharacterVisualLoadout
}

type CharacterPresentationProjector func(*domain.Character) CharacterPresentation
type CharacterCreationValidator func(*domain.Character) bool

// Character endpoints are the account-scoped application boundary over the
// authority store. Session middleware establishes account ownership before
// any handler reaches this file.
func (api *API) handleCharacterList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// Lazy reap: matured deletion reservations archive before every
	// character-select listing, so the screen never shows a character the
	// window already claimed.
	// Shard selection is an authenticated title-session claim. The query
	// divisionId is only a browser echo and has no authority.
	division := requestShardID(r)
	api.store.ReapMaturedDeletions()
	accountID := requestAccountID(r)
	var rows []map[string]interface{}
	api.store.ReadCharacterSelection(division, func(characters []*domain.Character, blockers map[int64]string) {
		rows = make([]map[string]interface{}, 0, len(characters))
		for _, c := range characters {
			if c.AccountID == accountID {
				row := api.createdCharacterJSON(c)
				if blocker := blockers[c.ID]; blocker != "" {
					row["deletionBlocker"] = blocker
				}
				rows = append(rows, row)
			}
		}
	})
	writeCharacterRosterResponse(w, map[string]interface{}{
		"action": 2, "nativeResult": 1, "characters": rows,
	})
}

func (api *API) handleNameOverlap(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// Lazy reap first: a matured-but-unreaped reservation must not answer
	// taken (0x11) for a name the 7-day window already freed.
	api.store.ReapMaturedDeletions()
	var request struct {
		CharacterName string `json:"characterName"`
		DivisionID    string `json:"divisionId"`
	}
	if err := decodeJSONRequest(r.Body, &request); err != nil || request.CharacterName == "" {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"action": 4, "nativeResult": 0, "nativeErrorCode": errCodeInvalidName,
		})
		return
	}
	division := requestShardID(r)
	if code, refused := api.nameRefusal(division, request.CharacterName); refused {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"action": 4, "nativeResult": 0, "nativeErrorCode": code,
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"action": 4, "nativeResult": 1})
}

// nameRefusal mirrors the create path's validation for the pre-check:
// shape first (0x0c), then case-insensitive overlap (0x11).
func (api *API) nameRefusal(division, name string) (int, bool) {
	if !store.CharacterNameShapeValid(name) {
		return errCodeInvalidName, true
	}
	taken := false
	api.store.ReadCharacters(division, func(characters []*domain.Character) {
		for _, existing := range characters {
			if strings.EqualFold(existing.Name, name) {
				taken = true
				return
			}
		}
	})
	if taken {
		return errCodeNameOverlap, true
	}
	return 0, false
}

// createRequest is the client's CPSCharacterCreateCreateRequest.
type createRequest struct {
	DivisionID     string `json:"divisionId"`
	CharacterName  string `json:"characterName"`
	ModelCodename  string `json:"modelCodename"`
	HeightIndex    *int64 `json:"heightIndex"`
	VolumeIndex    *int64 `json:"volumeIndex"`
	WeaponIndex    *int64 `json:"weaponIndex"`
	ProtectorIndex *int64 `json:"protectorIndex"`
	ArmorSelected  bool   `json:"armorSelected"`
	WeaponSelected bool   `json:"weaponSelected"`
}

func (api *API) handleCharacterCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// Lazy reap first: create's overlap check must not refuse a name whose
	// matured reservation the window already claimed (the same trigger the
	// list/delete-action/select-start handlers run).
	api.store.ReapMaturedDeletions()
	refuse := func(code int) {
		writeCharacterRosterResponse(w, map[string]interface{}{
			"action": 1, "nativeResult": 0, "nativeErrorCode": code,
		})
	}
	var request createRequest
	if err := decodeJSONRequest(r.Body, &request); err != nil {
		refuse(errCodeInvalidChargen)
		return
	}
	if request.CharacterName == "" || request.ModelCodename == "" ||
		request.HeightIndex == nil || request.VolumeIndex == nil ||
		request.WeaponIndex == nil || request.ProtectorIndex == nil {
		refuse(errCodeInvalidChargen)
		return
	}
	division := requestShardID(r)
	if code, refused := api.nameRefusal(division, request.CharacterName); refused {
		refuse(code)
		return
	}

	character := api.characterFromCreate(request)
	if !api.characterCreationValid(character) {
		refuse(errCodeInvalidChargen)
		return
	}
	if err := api.store.CreateCharacter(division, requestAccountID(r), character); err != nil {
		// The store's own validation is the authority; map its refusal
		// classes back onto the native table (the pre-checks above make
		// these rare races, not the normal path).
		switch {
		case errors.Is(err, store.ErrCharacterSlotsFull):
			refuse(0x05)
		case errors.Is(err, store.ErrCharacterNameConflict):
			refuse(errCodeNameOverlap)
		case errors.Is(err, store.ErrCharacterNameInvalid):
			refuse(errCodeInvalidName)
		default:
			log.Errorf("agentapi: create %q failed: %v", request.CharacterName, err)
			refuse(errCodeCreateFailed)
		}
		return
	}
	// The record is shared store state from CreateCharacter on; read it
	// back under the door like everyone else.
	var characterJSON map[string]interface{}
	api.store.ReadCharacters(division, func([]*domain.Character) {
		characterJSON = api.createdCharacterJSON(character)
	})
	writeCharacterRosterResponse(w, map[string]interface{}{
		"action": 1, "nativeResult": 1, "character": characterJSON,
	})
}

// characterFromCreate builds the persisted record. MissionInventory stays
// nil ON PURPOSE: nil means never-seeded, so the first enter-world runs
// the S8 starter seed (inventory + dev gold) exactly like every existing
// character did.
func (api *API) characterFromCreate(request createRequest) *domain.Character {
	level := int64(1)
	gold := int64(0)
	visualFlags := int64(domain.VisualFlagBeginner)
	heightIndex := coerceIndex(request.HeightIndex)
	volumeIndex := coerceIndex(request.VolumeIndex)
	// The client decodes heightIndex from the LOW nibble and volumeIndex
	// from the HIGH nibble (scaleFromBodyShapeByte / 0x32B3 parse).
	bodyShape := (volumeIndex << 4) | (heightIndex & 0x0f)
	c := &domain.Character{
		Name:           request.CharacterName,
		ModelCodename:  request.ModelCodename,
		HeightIndex:    request.HeightIndex,
		VolumeIndex:    request.VolumeIndex,
		WeaponIndex:    request.WeaponIndex,
		ProtectorIndex: request.ProtectorIndex,
		ArmorSelected:  request.ArmorSelected,
		WeaponSelected: request.WeaponSelected,
		BodyShapeByte:  &bodyShape,
		Level:          &level,
		Gold:           &gold,
		VisualFlags:    &visualFlags,
		CreatedAt:      api.now().UTC().Format(createdAtTimeFormatISO),
	}
	raceIndex := domain.ResolveCharacterRaceIndex(c)
	gender := domain.ResolveCharacterGenderIndex(c)
	c.RaceIndex = &raceIndex
	c.Gender = &gender
	return c
}

func coerceIndex(v *int64) int64 {
	if v == nil || *v < 0 {
		return 0
	}
	return *v
}

// deleteActionRequest is the client's CPSCharacterSelectDeleteRecoveryRequest:
// action 3 reserves a deletion, action 5 restores.
type deleteActionRequest struct {
	Action        int    `json:"action"`
	CharacterName string `json:"characterName"`
	DivisionID    string `json:"divisionId"`
}

func (api *API) handleDeleteAction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var request deleteActionRequest
	if err := decodeJSONRequest(r.Body, &request); err != nil ||
		(request.Action != 3 && request.Action != 5) || request.CharacterName == "" {
		writeCharacterRosterResponse(w, map[string]interface{}{
			"action": request.Action, "nativeResult": 0, "nativeErrorCode": errCodeServerConnect,
		})
		return
	}
	// Lazy reap first: a restore attempt on a reservation the window
	// already claimed must answer unknown-id, not resurrect the record.
	api.store.ReapMaturedDeletions()
	division := requestShardID(r)
	accountID := requestAccountID(r)
	character := api.findCharacter(division, accountID, request.CharacterName)
	if character == nil {
		writeCharacterRosterResponse(w, map[string]interface{}{
			"action": request.Action, "nativeResult": 0, "nativeErrorCode": errCodeUnknownID,
		})
		return
	}
	// A character bound to a live session cannot reserve deletion: the
	// retail client only composes deletes from character select, so an
	// in-play request is an un-composable shape and answers the silent
	// generic - the session is NOT kicked.
	if request.Action == 3 && api.characterInPlay != nil && api.characterInPlay(division, request.CharacterName) {
		writeCharacterRosterResponse(w, map[string]interface{}{
			"action": request.Action, "nativeResult": 0, "nativeErrorCode": errCodeServerConnect,
		})
		return
	}
	// The commit door's closure runs under the store lock: mutate AND
	// snapshot the response there, one lock, no second read.
	var characterJSON map[string]interface{}
	if request.Action == 3 {
		reservedAt := api.now().UTC().Format(createdAtTimeFormatISO)
		if !api.store.ReserveCharacterDeletion(character, reservedAt) {
			writeCharacterRosterResponse(w, map[string]interface{}{
				"action": request.Action, "nativeResult": 0, "nativeErrorCode": errCodeServerConnect,
			})
			return
		}
		api.store.ReadCharacters(division, func([]*domain.Character) {
			characterJSON = api.createdCharacterJSON(character)
		})
	} else {
		api.store.MutateCharacter(character, "delete-restore "+request.CharacterName, func() {
			character.DeletePending = false
			character.DeleteReservedAt = ""
			characterJSON = api.createdCharacterJSON(character)
		})
	}
	// POST-REAP ORPHAN GUARD: findCharacter resolved the pointer BEFORE
	// the mutate door, so a concurrent reap (another handler's lazy
	// trigger) can archive the record in between; the mutate closure then
	// ran on a detached record (the store warns "unknown character") and
	// answering success would confirm an action on a character that no
	// longer exists. Re-resolve under the read door; pointer identity also
	// rejects a same-name record recreated after the reap.
	if api.findCharacter(division, accountID, request.CharacterName) != character {
		writeCharacterRosterResponse(w, map[string]interface{}{
			"action": request.Action, "nativeResult": 0, "nativeErrorCode": errCodeUnknownID,
		})
		return
	}
	// IN-PLAY RE-CHECK (enter-world vs delete-reserve race): the guard
	// above the mutate runs BEFORE the reservation lands, so a bind that
	// completed in between would leave a live session playing a
	// DeletePending character. The reservation is committed (store lock)
	// before this Hub read, and the bind side re-reads DeletePending under
	// the store door AFTER its binding is visible (server.go OnWorldBound),
	// so at least one side always observes the other: both cannot succeed.
	// If the bind won, roll the reservation back and answer the same
	// silent generic as the pre-check. In a tight race both sides may
	// refuse (bind torn down AND reservation rolled back) - safe, the
	// client simply retries.
	if request.Action == 3 && api.characterInPlay != nil && api.characterInPlay(division, request.CharacterName) {
		api.store.MutateCharacter(character, "delete-reserve-rollback "+request.CharacterName, func() {
			character.DeletePending = false
			character.DeleteReservedAt = ""
		})
		writeCharacterRosterResponse(w, map[string]interface{}{
			"action": request.Action, "nativeResult": 0, "nativeErrorCode": errCodeServerConnect,
		})
		return
	}
	writeCharacterRosterResponse(w, map[string]interface{}{
		"action": request.Action, "nativeResult": 1, "character": characterJSON,
	})
}

// writeCharacterRosterResponse versions every response whose successful shape
// can carry a CharacterRosterRecord. The browser validates this field before
// any response reaches application state, making a mixed client/GameWorld
// deployment fail at the transport boundary instead of inside React.
func writeCharacterRosterResponse(w http.ResponseWriter, body map[string]interface{}) {
	body["characterRosterContractVersion"] = CharacterRosterContractVersion
	writeJSON(w, http.StatusOK, body)
}

// handleAgentPacket answers the select-start request (0x7426 -> 0xB426):
// the scene handoff gate. The actual enter-world rides the game
// transport; this only proves the character exists and is startable.
func (api *API) handleAgentPacket(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var request struct {
		NativeOpcode  int    `json:"nativeOpcode"`
		CharacterName string `json:"characterName"`
		DivisionID    string `json:"divisionId"`
	}
	refuse := func(code int) {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"nativeOpcode": selectStartRespOpcode, "nativeResult": 0, "nativeErrorCode": code,
		})
	}
	if err := decodeJSONRequest(r.Body, &request); err != nil || request.NativeOpcode != selectStartReqOpcode {
		refuse(errCodeServerConnect)
		return
	}
	api.store.ReapMaturedDeletions()
	division := requestShardID(r)
	found, startable := api.characterStartable(division, requestAccountID(r), request.CharacterName)
	if !found || !startable {
		refuse(errCodeUnknownID)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"nativeOpcode": selectStartRespOpcode,
		"nativeResult": 1,
		"nextScene":    nextSceneMission,
	})
}

// characterStartable resolves, under the read door, whether the named
// character exists and can enter the world (not delete-pending) — the
// same gate select-start and the EnterWorld token mint share.
func (api *API) characterStartable(division, accountID, name string) (found, startable bool) {
	api.store.ReadCharacters(division, func(characters []*domain.Character) {
		for _, c := range characters {
			if c.AccountID == accountID && strings.EqualFold(c.Name, name) {
				found = true
				startable = !c.DeletePending
				return
			}
		}
	})
	return found, startable
}

// handleEnterWorldToken mints the EnterWorld bind token the game
// transport's auth gate verifies (auth.Verify inside Hub.SetEnterWorldAuth;
// REV-5 S3/S6). This endpoint IS the login/character-select flow's mint
// point: every client bind path (launcher flow, dev deep-link, probe
// harnesses) funnels through the Go transport session, which fetches a
// fresh token here immediately before each 0x0006 frame. The character
// must exist and be startable — the same gate as select-start — so a
// token can never name a division/character the select screen would refuse.
//
// The signing secret is a construction prerequisite and arrives through the
// process composition (auth.EnvSecret), never a literal.
func (api *API) handleEnterWorldToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var request struct {
		CharacterName string `json:"characterName"`
		DivisionID    string `json:"divisionId"`
	}
	if err := decodeJSONRequest(r.Body, &request); err != nil || strings.TrimSpace(request.CharacterName) == "" {
		writeJSON(w, http.StatusOK, map[string]interface{}{"ok": false, "code": "BAD_REQUEST"})
		return
	}
	// Lazy reap first, like every character read on this surface: a
	// matured deletion must not receive a bind ticket.
	api.store.ReapMaturedDeletions()
	division := requestShardID(r)
	if !maintenanceAdmits(api.maintenanceGatePath, requestAccountID(r)) {
		writeJSON(w, http.StatusOK, map[string]interface{}{"ok": false, "code": "MAINTENANCE"})
		return
	}
	found, startable := api.characterStartable(division, requestAccountID(r), request.CharacterName)
	if !found || !startable {
		log.WithField("character", request.CharacterName).
			Warn("agentapi: EnterWorld token refused (unknown or delete-pending character)")
		writeJSON(w, http.StatusOK, map[string]interface{}{"ok": false, "code": "UNKNOWN_CHARACTER"})
		return
	}
	expiresAt := api.now().Add(enterWorldTokenTTL)
	token, err := auth.Mint(api.enterWorldAuthSecret, division, request.CharacterName, expiresAt)
	if err != nil {
		log.Errorf("agentapi: EnterWorld token mint for %q failed: %v", request.CharacterName, err)
		writeJSON(w, http.StatusOK, map[string]interface{}{"ok": false, "code": "INTERNAL"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok":            true,
		"token":         token,
		"expiresAtUnix": expiresAt.Unix(),
	})
}

// findCharacter resolves a name to the store's shared record under the
// read door. Names are immutable, so the returned POINTER stays valid;
// its fields must be read back under a door.
func (api *API) findCharacter(division, accountID, name string) *domain.Character {
	var found *domain.Character
	api.store.ReadCharacters(division, func(characters []*domain.Character) {
		for _, c := range characters {
			if c.AccountID == accountID && strings.EqualFold(c.Name, name) {
				found = c
				return
			}
		}
	})
	return found
}

// createdCharacterJSON keeps persisted creation controls as provenance, but
// exposes exactly one assembled visual answer. Consumers must never rebuild a
// second appearance by merging the raw controls back into visualLoadout.
func (api *API) createdCharacterJSON(c *domain.Character) map[string]interface{} {
	presentation := api.characterPresentation(c)
	presentation.VisualLoadout = characterVisualLoadoutForWire(presentation.VisualLoadout)
	out := map[string]interface{}{
		"id":             c.ID,
		"name":           c.Name,
		"level":          coerceOr(c.Level, 1),
		"raceIndex":      presentation.RaceIndex,
		"gender":         presentation.Gender,
		"figureIndex":    coerceOr(c.FigureIndex, 0),
		"heightIndex":    coerceOr(c.HeightIndex, 0),
		"volumeIndex":    coerceOr(c.VolumeIndex, 0),
		"weaponIndex":    coerceOr(c.WeaponIndex, 0),
		"protectorIndex": coerceOr(c.ProtectorIndex, 0),
		"armorSelected":  c.ArmorSelected,
		"weaponSelected": c.WeaponSelected,
		"deletePending":  c.DeletePending,
		"visualLoadout":  presentation.VisualLoadout,
	}
	if c.BodyShapeByte != nil {
		out["bodyShapeByte"] = *c.BodyShapeByte
	}
	if presentation.ExperiencePercent != nil {
		out["experiencePercent"] = *presentation.ExperiencePercent
	}
	if c.SkillPoints != nil {
		out["skillPoints"] = *c.SkillPoints
	}
	if c.CurrentHP != nil {
		out["currentHp"] = *c.CurrentHP
	}
	if c.CurrentMP != nil {
		out["currentMp"] = *c.CurrentMP
	}
	// Maxima are DERIVED (bootstrap/charactervitals/vitals.go), never the persisted
	// history fields: the select screen must show the same maxima the
	// in-world 0x343C block carries. Always present - the formula needs
	// only level + STR/INT, which every record can answer via fallbacks.
	out["maxHp"] = charactervitals.DerivedMaxHP(c)
	out["maxMp"] = charactervitals.DerivedMaxMP(c)
	if c.DeleteReservedAt != "" {
		out["deleteReservedAt"] = c.DeleteReservedAt
	}
	if c.World != nil {
		out["world"] = characterWorldJSON(c.World)
	}
	return out
}

/*
================
characterVisualLoadoutForWire

Closes Go's nil-slice/JSON-null seam at the HTTP owner: item lists are
arrays in the roster contract even when nothing is worn, so clients never
read null as a second spelling of an empty list.
================
*/
func characterVisualLoadoutForWire(loadout CharacterVisualLoadout) CharacterVisualLoadout {
	loadout.Items = append(make([]CharacterItem, 0, len(loadout.Items)), loadout.Items...)
	loadout.Avatars = append(make([]CharacterItem, 0, len(loadout.Avatars)), loadout.Avatars...)
	return loadout
}

// characterWorldJSON maps the persisted character.world record onto the
// launcher JSON's world block: the settled spawn (regionId/x/y/z/angle)
// plus movementMode - the fields the probe harnesses' "start position"
// readers consume (character.world.spawn, rebuild/scripts/lib/
// browserProbe.mjs readPersistedCharacterPosition). The field went
// missing in the store refactor; this restores the pre-refactor shape.
// The runtime-only planes (moveSegment echo, bookkeeping stamps) stay
// off the select-screen wire.
//
// The spawn field pointers land in the map as-is (nil marshals as null,
// matching the persisted WorldSpawn json tags). Safe to marshal after
// the read door closes: the movement lanes' write-backs copy-then-swap
// the World/Spawn structs and never mutate the old values in place.
func characterWorldJSON(world *domain.CharacterWorld) map[string]interface{} {
	out := map[string]interface{}{}
	if world.Spawn != nil {
		out["spawn"] = map[string]interface{}{
			"regionId": world.Spawn.RegionID,
			"x":        world.Spawn.X,
			"y":        world.Spawn.Y,
			"z":        world.Spawn.Z,
			"angle":    world.Spawn.Angle,
		}
	} else {
		out["spawn"] = nil
	}
	if world.MovementMode != nil {
		out["movementMode"] = *world.MovementMode
	}
	return out
}

func coerceOr(v *int64, fallback int64) int64 {
	if v == nil {
		return fallback
	}
	return *v
}
