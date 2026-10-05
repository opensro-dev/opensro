/*
===========================================================================

effects.ts - the effect catalog and the visuals the effect owner draws

Shapes shared by the asset pipeline's effect records, the effect owner
(runtime/characters/effects) and its consumers: catalog stages, live
visuals with their attachment and release state, flights, impacts and the
triggers that start stage effects.

===========================================================================
*/
import type { CharacterActor } from "./character";
/*
================
HawkAnimation
One animation state of the Rogue hawk model.
================
*/
export interface HawkAnimation {
	readonly clip: string;
	readonly durationMs: number;
	readonly loop: boolean;
	readonly trackEvents: readonly { readonly cursorMs: number; readonly eventCode: number; }[];
}
/*
================
HawkImpact
A hawk strike reaching its target, reported for hit feedback.
================
*/
export interface HawkImpact {
	readonly resultKey: string;
	readonly id: number;
	readonly holder: number;
	readonly target: number;
	readonly skill: number;
	readonly damage: number;
	readonly at: number;
}
/*
================
EffectStage
One authored skilleffectset row: phase, event, action, movement,
resource, bones and native command options.
================
*/
export interface EffectStage {
	readonly script?: import("@/engine/foundation/animation/effect-script").EffectScript;
	readonly native?: {
		readonly slot: number;
		readonly attach: number;
		readonly trade: number;
		readonly kill: number;
		readonly scale: "MOB_BASE" | "CHAR_BASE" | null;
		readonly rotation: number;
		readonly fadeInMs: number;
		readonly fadeOutMs: number;
		readonly damageTypes: readonly string[];
		readonly actionOptions: {
			readonly enabled: boolean;
			readonly direction: number;
			readonly distance: number;
			readonly residualDistance: number;
			readonly simultaneousRelease: boolean;
		};
	};
	readonly parameters?: readonly [number, number, number];
	readonly phase?: string;
	readonly movement?: { readonly delayMs: number; readonly startSpeed: number; readonly endSpeed: number; };
	readonly targetBone?: string | null;
	readonly targetOffset?: readonly [number, number, number];
	readonly arrivalResource?: string | null;
	readonly soundEnd?: string | null;
	readonly resource: string | null;
	readonly damageEvent: boolean;
	readonly startEvent: number;
	readonly action: string;
	readonly move: string;
	readonly bone: string | null;
	readonly offset: readonly [
		number,
		number,
		number
	];
	readonly life: number;
	readonly sound: string | null;
	readonly count: number;
	readonly scripts: readonly string[];
}
/*
================
EffectRecord
A skill's compiled effect record: clips, stages and related resources.
================
*/
export interface EffectRecord {
	readonly hitLight?: {
		readonly color: readonly [number, number, number];
		readonly duration: number;
		readonly range: number;
		readonly attenuation: number;
	};
	readonly attachedMotion?: { readonly set: string; readonly id: number; };
	readonly damageEffect?: string | null;
	readonly secondaryEffect?: boolean;
	readonly arrowEffects?: readonly [string | null, string | null];
	readonly hideWeapon?: number;
	readonly overlap?: boolean;
	readonly attachedAction?: import("@/engine/foundation/animation/impact-source").AttachedAction;
	readonly phaseClips?: readonly (readonly string[])[];
	readonly clips: readonly string[];
	readonly stages: readonly EffectStage[];
}
/*
================
EffectCatalog
Effect records by skill id (or by name in the named catalog).
================
*/
export type EffectCatalog = Readonly<Record<string, EffectRecord>>;
/*
================
EffectAttachment
Where a visual lives: fixed in the world, or following an entity with
an offset (and optionally a fixed facing).
================
*/
export type EffectAttachment = {
	readonly kind: "world";
} | {
	readonly kind: "entity";
	readonly offset: readonly [
		number,
		number,
		number
	];
	// A fixed yaw (radians) instead of the anchor's heading: 8D5440 gives a
	// victim-anchored hit effect the caster's matrix at spawn.
	readonly facing?: import("@/engine/foundation/math/angles").Radians;
};
/*
================
EffectRelease
Fade-out state of a released looping or stopping command.
================
*/
export interface EffectRelease {
	readonly fade: number;
	released?: number;
	previous?: number;
	progress?: number;
	opacity?: number;
}
/*
================
EffectVisual
One live effect actor with its lifetime, attachment, command and flight.
================
*/
export interface EffectVisual {
	readonly landed?: { readonly at: number; };
	readonly auxiliary?: {
		actor: CharacterActor;
		readonly fade: number;
		previous: number;
		progress: number;
		opacity: number;
		started?: number;
		expired: boolean;
	}[];
	readonly command?: EffectRelease & { readonly slot: number; readonly loop: boolean; };
	readonly visualStarted?: number;
	readonly family?: {
		readonly fadeIn: number;
		readonly fadeOut: number;
		readonly stopEmission: boolean;
		end: { readonly at: number; readonly remove: boolean; } | null;
		previous: number;
		progress: number;
		opacity: number;
	};
	readonly camera?: {
		readonly target: number;
		readonly script: Omit<import("./camera-script").CameraScript, "atMs">;
	};
	readonly independent?: boolean;
	readonly impact?: {
		readonly cast: import("./gameplay").CastState;
		readonly target: number;
		readonly index: number;
		readonly soundSkill: number;
		readonly allTargets?: boolean;
		readonly secondary?: boolean;
		readonly atTarget?: boolean;
	};
	readonly actor: CharacterActor;
	readonly attachment: EffectAttachment;
	readonly owner: number;
	readonly token: number;
	readonly life: number;
	readonly flight?: EffectFlight;
}

/*
================
EffectFlight
A moving stage effect: curve or arc, destination, speed and arrival.
================
*/
export interface EffectFlight {
	orientation?: CharacterActor["pose"];
	readonly curve?: {
		readonly state: import("@/engine/foundation/animation/projectile-curve").ProjectileCurve;
		previousMs: number;
	};
	readonly arc?: import("@/engine/foundation/animation/projectile-time").ProjectileArc;
	destination: CharacterActor["pose"];
	readonly speed: number;
	readonly delay: number;
	readonly arrivalResource: string | null;
	readonly soundEnd: string | null;
	soundBegin: string | null;
	// A shot at one target that follows it (stepHomingProjectile): the
	// target's live socket replaces destination every frame.
	readonly homing?: {
		readonly state: import("@/engine/foundation/animation/projectile-time").HomingProjectile;
		readonly target: number;
		readonly bone: string | null;
		readonly offset: readonly [number, number, number];
		readonly trigger: EffectTrigger;
	};
	readonly moving?: {
		pose: CharacterActor["pose"];
		previous: number;
		delay: number;
		travelled: number;
		age: number;
		readonly route: { readonly kind: "radial"; readonly spacing: number; } | {
			readonly kind: "chain";
			readonly targets: readonly number[];
			cursor: number;
			pending: number | null;
			readonly trigger: EffectTrigger;
			readonly bone: string | null;
			readonly offset: readonly [number, number, number];
		};
	};
}

/*
================
EffectImpactEvent
A flight launch or arrival that result feedback waits for.
================
*/
export interface EffectImpactEvent {
	readonly position?: CharacterActor["pose"];
	readonly secondary?: boolean;
	readonly atTarget?: boolean;
	readonly allTargets?: boolean;
	readonly kind: "launch" | "arrival" | "discard" | "hop" | "skip";
	readonly flight: number;
	readonly cast: import("./gameplay").CastState;
	readonly target: number;
	readonly index: number;
	readonly at: number;
	readonly soundSkill?: number;
}

/*
================
EffectTrigger
A cast phase event that starts stage effects.
================
*/
export interface EffectTrigger {
	readonly sampleCurrent?: boolean;
	readonly attackKind?: number;
	readonly cast: import("./gameplay").CastState;
	readonly phase: string;
	readonly event: number;
	readonly at: number;
}
