/*
===========================================================================

character.ts - the character model, actor and attachment contracts

What the asset worker delivers for a character, item or effect source
(CharacterModel, with retained bitmaps or native mip blocks beside it) and what the
page asks the renderer to draw (CharacterActor). CharacterSource is the
worker-only form that still holds encoded image containers.

===========================================================================
*/
import type { Geometry } from "./geometry";
/*
================
CharacterNode

Authored skeleton hierarchy and local rest transforms.
================
*/
export interface CharacterNode {
	readonly name: string;
	readonly parent: number;
	readonly translation: readonly number[];
	readonly rotation: readonly number[];
	readonly scale: readonly number[];
	readonly matrix?: readonly number[];
}
/*
================
CharacterChannel

One typed animation channel targeting an authored skeleton node.
================
*/
export interface CharacterChannel {
	readonly node: number;
	readonly path: "translation" | "rotation" | "scale";
	readonly interpolation: "LINEAR" | "STEP" | "CUBICSPLINE";
	readonly times: Float32Array;
	readonly values: Float32Array;
}
/*
================
CharacterClip

A named set of channels sharing the authored clip duration.
================
*/
export interface CharacterClip {
	readonly name: string;
	readonly duration: number;
	readonly channels: readonly CharacterChannel[];
}
/*
================
CharacterPrimitive

Geometry and native material bindings for one skinned or effect primitive.
================
*/
export interface CharacterPrimitive {
	readonly cloth?: import("@/engine/foundation/animation/cloth").ClothData;
	readonly modifierSource?: {
		readonly material: import("./scene").WorldMaterial;
		readonly index: number;
		readonly modifiers: import("@/engine/foundation/rendering/scenery-modifiers").SceneryModifiers;
	};
	readonly particleEmitter?: number;
	/** spline: only RenderLinkPipe smooths its chain (AF9020); LinkDPipe
	 * (AF8E80) and LinkObj (AF73A0) draw the raw chain. */
	readonly ribbon?: { readonly widths: Float32Array; readonly fps: number; readonly spline: boolean; };
	readonly emission?: {
		readonly capacity?: number;
		readonly loop?: boolean;
		readonly births: readonly number[];
		readonly lifetime: number;
		readonly follow?: boolean;
	};
	readonly particleProgram?: import("@/engine/foundation/animation/particle-program").ParticleProgram;
	readonly billboard?: "camera" | "y" | "v";
	readonly materialFrames?: {
		readonly sampling?: "step";
		readonly fps: number;
		readonly colors: Float32Array;
		readonly windows: Float32Array;
	};
	readonly name: string;
	readonly node: number;
	readonly joints: readonly number[];
	readonly inverseBind: Float32Array;
	readonly geometry: Geometry;
	readonly image: number;
	readonly environmentImage?: number;
	readonly equipmentGlow?: import("@/engine/foundation/rendering/equipment-glow").EquipmentGlow;
}
/*
================
CharacterModel

Immutable scene data delivered separately from its owned texture resources.
================
*/
export interface CharacterModel {
	// The base resource's authored box (CResObject +0x280) in model space:
	// the native pick box. Absent on models with no resource (effects).
	readonly aggregateBox?: import("@/engine/foundation/rendering/picking").PickBounds;
	readonly equipmentGlows?: Record<
		string,
		readonly import("@/engine/foundation/rendering/equipment-glow").EquipmentGlow[]
	>;
	readonly particleGraph?: readonly import("@/engine/foundation/animation/particle-graph").ParticleEmitter[];
	readonly nodes: readonly CharacterNode[];
	readonly primitives: readonly CharacterPrimitive[];
	readonly clips: readonly CharacterClip[];
	// Textures arrive beside the model as bitmaps or native mip resources.
	// Only dimensions live here; encoded PNGs never leave the asset worker.
	readonly images: readonly CharacterImage[];
}
/*
================
CharacterImage

Dimensions retained for admission checks without retaining encoded PNG bytes.
================
*/
export interface CharacterImage {
	readonly width: number;
	readonly height: number;
}
// A decoded GLB or effect program inside the asset worker, before its
// embedded images become bitmaps or transferable native mip levels.
/*
================
CharacterSource

Worker-local model data before image decoding and ownership transfer.
================
*/
export interface CharacterSource extends Omit<CharacterModel, "images"> {
	readonly images: readonly {
		readonly bytes: Uint8Array;
		readonly mime: string;
	}[];
}
/*
================
CharacterLayer

An event or timed animation layer with explicit activation and blend weight.
================
*/
export interface CharacterLayer {
	readonly rate?: number;
	readonly clip: string;
	readonly time: number;
	readonly loop: boolean;
	readonly weight: number;
	readonly lane: "event" | "timed";
	readonly activation?: import("@/engine/foundation/animation/animation-activation").AnimationActivation;
}
/*
================
CharacterPointLight

Native actor-light inputs projected into the scene region.
================
*/
export interface CharacterPointLight {
	readonly pose: { readonly regionId: number; readonly x: number; readonly y: number; readonly z: number; };
	readonly ambient: readonly [number, number, number];
	readonly diffuse: readonly [number, number, number];
	readonly attenuation: number;
	readonly range: number;
}
/*
================
CharacterActor

Per-instance render state referencing a resident model rather than owning one.
================
*/
export interface CharacterActor {
	/** Native reference field +0x110; absent for non-character effects/items. */
	readonly shadowSize?: number;
	readonly shadowAttachment?: boolean;
	readonly modifierId?: number;
	readonly modelAnimation?: import("@/engine/foundation/animation/model-animation").ModelAnimationFrame;
	readonly animationLod?: { readonly fraction: number; readonly crowded: boolean; readonly optional?: boolean; };
	readonly deferredParticle?: {
		readonly offset: number;
		readonly nightOnly?: boolean;
		readonly lodHidden?: boolean;
	};
	/** Retail CICMonster/CICUser class membership; retained through corpse departure. */
	readonly blindable?: boolean;
	/** CIItem labels anchor five world units above the ground, independent of model bounds. */
	readonly groundItem?: boolean;
	readonly pointLight?: CharacterPointLight;
	readonly bloodEffects?: readonly [string | null, string | null];
	/** Equipment-selected idle for the independently animated inventory model. */
	readonly previewClip?: string;
	readonly effectBasis?: readonly [number, number, number, number, number, number, number, number, number];
	/** Keep the socket transform alive after stopping a projectile model. */
	readonly drawGeometry?: boolean;
	readonly materialTint?: readonly [number, number, number];
	readonly heightFactor?: number;
	readonly effectBaseScale?: number;
	/** EFP size is absolute; the socket supplies position/orientation only. */
	readonly absoluteEffectScale?: boolean;
	/** Stop new particle births at this actor-local time; existing particles finish. */
	readonly emissionEnd?: number;
	readonly effectRotation?: { readonly axis: "x" | "y" | "z"; readonly angle: number; };
	readonly effectAnchor?: { readonly bone: string | null; readonly offset: readonly [number, number, number]; };
	readonly height?: number;
	readonly pickable?: boolean;
	/** An unpickable actor a filtered pick still takes: a dead player, which
	 * World_PickEntityAtScreenPoint (692680) admits when the world click
	 * holds SHIFT or the local player is dead (698740's filter flag). */
	readonly pickWhenFiltered?: boolean;
	/** A linked ride's rider: a pick on the ride answers with this gid
	 * (World_PickEntityAtScreenPoint 692680 reads the ride's +0x2A4). */
	readonly pickOwner?: number;
	readonly bodyVolume?: { readonly index: number; readonly female: boolean; };
	readonly opacity?: number;
	/** A skill/orb effect entity: its owner's model fades do not reach it
	 * (character-fade.ts attachedOpacity). */
	readonly effectEntity?: boolean;
	readonly layers?: readonly CharacterLayer[];
	readonly attachment?: {
		readonly gid: number;
		readonly bone: string;
		readonly offset: readonly [number, number, number];
		readonly rootIfMissing?: boolean;
		readonly root?: boolean;
		/** native: the 8D6880 holder matrix for an .efp program; native-bsr: the
		 * same matrix for a compiled (Z-flipped) BSR mesh. */
		readonly basis?: "native" | "native-bsr" | "bsr" | "compound";
		/** native / native-bsr named bones: false is binding +0x08 == 0 ('@Bone'),
		 * where 8D6880 replaces the bone and root rotation with identity and
		 * keeps only the position. Absent keeps the rotation. */
		readonly keepRotation?: boolean;
		readonly modelScale?: number;
		readonly rotation?: Float32Array;
		/** Root attachments only: a fixed world yaw replacing the owner's rotation
		 * (a victim-anchored hit effect keeps the caster's facing, 8D5440). */
		readonly facing?: import("@/engine/foundation/math/angles").Radians;
		/** Root attachments only: the effect stands at the character's ground
		 * position. For a rider that is the ride's root, not the saddle. */
		readonly ground?: boolean;
	};
	readonly mountedOn?: number;
	readonly gid: number;
	readonly model: string;
	readonly pose: Omit<import("./gameplay").Pose, "angle"> & {
		readonly yaw: import("@/engine/foundation/math/angles").Radians;
	};
	readonly clip: string;
	readonly time: number;
	readonly loop: boolean;
	readonly scale: number;
}
/*
================
CharacterAttachment

Equipment assembly references and the slots or branches they replace.
================
*/
export interface CharacterAttachment {
	readonly branches?: {
		readonly slot: number;
		readonly entries: readonly import("@/engine/foundation/animation/equipment-sockets").EquipmentBranch[];
	};
	readonly equipment?: { readonly refObjId: number; readonly plus: number; };
	readonly model: string;
	readonly parts: readonly string[];
	readonly covers: readonly number[];
}
