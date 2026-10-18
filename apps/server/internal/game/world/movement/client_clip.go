/*
===========================================================================

client_clip.go - ClientClip mode posture, step admission and clip telemetry

Owns the runtime wrapper around ClipPathValidator that enforces the configured
SRO_MOVE_CLIENT_CLIP posture (apply, observe, off) on ground moves and tracks
clip outcome counters.

===========================================================================
*/
package movement

import (
	"os"
	"sync/atomic"

	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/game/world/simulation"
)

// EnvMoveClientClip selects the clip posture:
//
//	unset / ""             -> APPLY (secure production default)
//	"observe"              -> compute + log the would-clip, move nobody
//	"off"                  -> clip code inert
//	"apply"                -> clip the committed goal to the first blocking
//	                          contact
//	anything else          -> APPLY with a loud warning (fail closed)
const EnvMoveClientClip = "SRO_MOVE_CLIENT_CLIP"

// ClipMode is the parsed posture.
type ClipMode string

const (
	ClipOff     ClipMode = "off"
	ClipObserve ClipMode = "observe"
	ClipApply   ClipMode = "apply"
)

/*
================
ClipModeFromEnv

ClipModeFromEnv defaults to applying the server-authoritative clip.
================
*/
func ClipModeFromEnv() ClipMode {
	switch value := os.Getenv(EnvMoveClientClip); value {
	case "", string(ClipApply):
		return ClipApply
	case string(ClipObserve):
		return ClipObserve
	case string(ClipOff):
		return ClipOff
	default:
		log.Warnf("movement: clientclip %s=%q unrecognized (want off|observe|apply); defaulting to apply", EnvMoveClientClip, value)
		return ClipApply
	}
}

/*
================
ClipPathValidator

ClipPathValidator is the seam ClientClip runs on (WaterValidator
implements it; tests fake it).
================
*/
type ClipPathValidator interface {
	ClipMovementPath(from, to simulation.Spawn) ClipReport
}

/*
================
ClientClipStats

ClientClipStats snapshots the clip's counters since boot.
================
*/
type ClientClipStats struct {
	Inspected     uint64
	Arrived       uint64
	WouldClip     uint64
	Applied       uint64
	NoCoverage    uint64
	DungeonExempt uint64
	StartBlocked  uint64
	Truncated     uint64
	// WouldClipObject is the object-class subset of WouldClip (the two
	// classes carry different client rest semantics; file banner).
	WouldClipObject uint64
	// ObjectDeckOverrides totals blocked-tile probes overridden by an
	// object-deck stand (same semantics as the pathguard counter).
	ObjectDeckOverrides uint64
}

/*
================
ClientClip

ClientClip computes the client-replicated hard-stop for every accepted
ground move. In observe mode it logs the would-clip and returns
the goal UNCHANGED - it must never move a player. In apply mode
(human-gated, Q6) it returns the clipped rest as the goal to commit.
================
*/
type ClientClip struct {
	Mode      ClipMode
	Validator ClipPathValidator

	inspected       atomic.Uint64
	arrived         atomic.Uint64
	wouldClip       atomic.Uint64
	applied         atomic.Uint64
	noCoverage      atomic.Uint64
	dungeonExempt   atomic.Uint64
	startBlocked    atomic.Uint64
	truncated       atomic.Uint64
	wouldClipObject atomic.Uint64
	deckOverrides   atomic.Uint64
}

/*
================
NewClientClipFromEnv

NewClientClipFromEnv builds the clip over the shared walkability plane.
Returns nil when configured off; logs the boot posture loudly so a
deployment is auditable from its first log lines.
================
*/
func NewClientClipFromEnv(validator ClipPathValidator) *ClientClip {
	mode := ClipModeFromEnv()
	if mode == ClipOff {
		log.Warnf("movement: clientclip OFF (%s=off) - would-clip telemetry disabled", EnvMoveClientClip)
		return nil
	}
	if mode == ClipApply {
		log.Infof("movement: clientclip APPLY (default) - ground moves stop at the first blocking contact")
	} else {
		log.Warnf("movement: clientclip OBSERVE - wall clipping is diagnostic-only")
	}
	return &ClientClip{Mode: mode, Validator: validator}
}

/*
================
ProcessMove

ProcessMove computes the clip for one mode-1 ground move (from = live
departure, to = the normalized goal ApplyMove would commit) and returns
the goal to actually commit.

OBSERVE INVARIANT (Q6 amendment 3, witnessed by test): in every mode
except apply, the returned spawn is the input `to`, bit-for-bit - the
observe path computes, logs, and discards.
================
*/
func (c *ClientClip) ProcessMove(characterName string, from, to simulation.Spawn) simulation.Spawn {
	return c.ProcessMoveFrom(characterName, from, simulation.NavOwner{}, to)
}

/*
================
ProcessMoveFrom

ProcessMoveFrom clips the chord walked from the mover's retained source
owner (navowner.go). The observe invariant is unchanged: only apply mode
returns anything but `to`.
================
*/
func (c *ClientClip) ProcessMoveFrom(characterName string, from simulation.Spawn, fromOwner simulation.NavOwner, to simulation.Spawn) simulation.Spawn {
	pose, _ := c.ProcessStepFrom(characterName, from, fromOwner, to)
	return pose
}

/*
================
ProcessStepFrom

The applied collision outcome is independent of coordinate displacement:
retail can accept the requested point and still return the native stop bit.
================
*/
func (c *ClientClip) ProcessStepFrom(characterName string, from simulation.Spawn, fromOwner simulation.NavOwner, to simulation.Spawn) (simulation.Spawn, bool) {
	pose, _, blocked := c.processOwnedStep(characterName, from, fromOwner, to)
	return pose, blocked
}

/*
================
processOwnedStep

A linked-cell placement is an accepted step, although native repair can
change XZ. Return its receiving owner instead of inferring a new owner from
the repaired point or treating every displacement as a stop.
================
*/
func (c *ClientClip) processOwnedStep(characterName string, from simulation.Spawn, fromOwner simulation.NavOwner, to simulation.Spawn) (simulation.Spawn, simulation.NavOwner, bool) {
	if c == nil || c.Validator == nil || c.Mode == ClipOff {
		return to, simulation.NavOwner{}, false
	}

	var report ClipReport
	if owned, ok := c.Validator.(ownerClipValidator); ok {
		report = owned.ClipMovementPathFrom(from, fromOwner, to)
	} else {
		report = c.Validator.ClipMovementPath(from, to)
	}
	total := c.inspected.Add(1)

	switch report.Outcome {
	case ClipArrived:
		c.arrived.Add(1)
	case ClipBlocked:
		c.wouldClip.Add(1)
		if report.Class == ClipClassObject {
			c.wouldClipObject.Add(1)
		}
	case ClipNoCoverage:
		c.noCoverage.Add(1)
	case ClipDungeonExempt:
		c.dungeonExempt.Add(1)
	case ClipStartBlocked:
		c.startBlocked.Add(1)
	}
	if report.Truncated {
		c.truncated.Add(1)
	}
	if report.ObjectDeckOverrides > 0 {
		c.deckOverrides.Add(uint64(report.ObjectDeckOverrides))
	}

	applying := report.Outcome == ClipBlocked && c.Mode == ClipApply

	if report.Outcome == ClipBlocked {
		log.WithFields(log.Fields{
			"guard":         "clientclip",
			"char":          characterName,
			"mode":          string(c.Mode),
			"applying":      applying,
			"class":         string(report.Class),
			"fromRegion":    from.RegionID,
			"fromX":         from.X,
			"fromZ":         from.Z,
			"toRegion":      to.RegionID,
			"toX":           to.X,
			"toZ":           to.Z,
			"restRegion":    report.Rest.RegionID,
			"restX":         report.Rest.X,
			"restZ":         report.Rest.Z,
			"blockedTile":   [2]int{report.BlockedTileX, report.BlockedTileZ},
			"tiles":         report.TilesChecked,
			"uncovered":     report.TilesUncovered,
			"deckOverrides": report.ObjectDeckOverrides,
		}).Debug("movement: clientclip would-clip")
	}

	if total == 1 || total%pathGuardSummaryEvery == 0 {
		c.logSummary(total)
	}

	if applying {
		c.applied.Add(1)
		return report.Rest, report.RestOwner, true
	}
	// A continued step (a linked cell, or an outline exit 98B300 ended
	// within 5 units) stands where the leg reached, on that leg's owner.
	// A dungeon arrival stands there too: clipDungeonPath reports it with
	// NativeResult 0, and its Rest.Y is the floor the step reached - dropping
	// it kept the server at the ramp-top height while the client descended,
	// so every later dungeon clip started over 2 m off its floor.
	if c.Mode == ClipApply && report.Outcome == ClipArrived && (report.NativeResult&nativeContinueResult != 0 || simulation.IsDungeonRegion(report.Rest.RegionID)) {
		return report.Rest, report.RestOwner, false
	}
	return to, simulation.NavOwner{}, false
}

/*
================
Stats

Stats snapshots the counters (test + telemetry surface).
================
*/
func (c *ClientClip) Stats() ClientClipStats {
	return ClientClipStats{
		Inspected:           c.inspected.Load(),
		Arrived:             c.arrived.Load(),
		WouldClip:           c.wouldClip.Load(),
		Applied:             c.applied.Load(),
		NoCoverage:          c.noCoverage.Load(),
		DungeonExempt:       c.dungeonExempt.Load(),
		StartBlocked:        c.startBlocked.Load(),
		Truncated:           c.truncated.Load(),
		WouldClipObject:     c.wouldClipObject.Load(),
		ObjectDeckOverrides: c.deckOverrides.Load(),
	}
}

/*
================
logSummary
================
*/
func (c *ClientClip) logSummary(total uint64) {
	stats := c.Stats()
	log.WithFields(log.Fields{
		"guard":           "clientclip",
		"inspected":       total,
		"arrived":         stats.Arrived,
		"wouldClip":       stats.WouldClip,
		"wouldClipObject": stats.WouldClipObject,
		"applied":         stats.Applied,
		"noCoverage":      stats.NoCoverage,
		"dungeonExempt":   stats.DungeonExempt,
		"startBlocked":    stats.StartBlocked,
		"truncated":       stats.Truncated,
		"deckOverrides":   stats.ObjectDeckOverrides,
		"mode":            string(c.Mode),
	}).Info("movement: clientclip summary")
}
