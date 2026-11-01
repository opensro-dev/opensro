/*
===========================================================================

runtime.ts - the runtime contracts

The interfaces the runtime owners implement and call across: the platform
(page, input, preferences, overlays), the renderer, and the runtime control
surface. Types only.

===========================================================================
*/
import type { UiTexture, WorldTexture } from "@/engine/contracts/texture";

import type { Geometry } from "./geometry";
import type { SessionCommand, SessionState } from "./session";
import type { InputBatch } from "./input";
/*
================
CharacterStatistics

Copied renderer counters. Retained poses include socket-only consumers and
shared evaluators, so eligibility is not a visible-body count or GPU admission.
================
*/
export interface CharacterStatistics {
	readonly actors: number;
	readonly draws: number;
	readonly visibleActors?: number;
	readonly poseEvaluations?: number;
	readonly liveOwnedCpuEvaluations?: number;
	/** CPU pose evaluations since the renderer began, retired evaluators included: never decreases. */
	readonly cpuEvaluations?: number;
	readonly gpuAnimation?: {
		readonly poses: number;
		readonly dispatches: number;
		readonly cpuUploadBytes?: number;
	};
	readonly poseEligibility?: {
		readonly actors: number;
		readonly unique: number;
		readonly gpuSamples: number;
		readonly linearSamples: number;
		readonly sharedPaletteSamples: number;
		readonly clothSamples: number;
		readonly gpuPaletteSamples: number;
	};
	/**
	 * The last main frame's admitted actors against their culling spheres:
	 * how many have their sphere centre inside the frustum, and for the rest
	 * how far outside it sits, in quarters of the radius (outsideShares[0]
	 * under 1/4 ... [3] 3/4 or more). Diagnostics only; no bound is implied safe.
	 */
	readonly cullSlack?: {
		readonly admitted: number;
		readonly bodies: number;
		readonly attachments: number;
		readonly centreInside: number;
		readonly outsideShares: readonly number[];
		readonly meanRadius: number;
		/** Lone bodies the active-clip candidate radius would reject (cloth not yet included). */
		readonly activeRejected: number;
		readonly meanActiveRadius: number;
		/**
		 * Lone bodies whose displayed skinned-pose AABB lies wholly outside every
		 * frustum: a diagnostic ceiling for tighter culling, not a safety proof. It
		 * excludes live cloth deformation, emissions and independent shadow casters.
		 */
		readonly posedHidden?: number;
		/** Lone bodies the ceiling could not judge (no resident pose or unskinned parts): counted visible. */
		readonly posedUnknown?: number;
		/** Of posedHidden, bodies with cloth, judged on its skinned rest shape. */
		readonly posedCloth?: number;
	};
}
/*
================
RuntimePhase

Every runtime owner exposes its explicit startup, running and retirement phase.
================
*/
export type RuntimePhase = "starting" | "running" | "failed" | "disposed";

/*
================
RenderFrameProbe

Optional synchronous measurements supplied by the frame owner. Renderer
modules never discover diagnostic globals or require rewritten source.
================
*/
export interface RenderFrameProbe {
	renderBegin(): void;
	renderMark( stage: string ): void;
	characterBegin(): void;
	characterMark( stage: string ): void;
	characterCount( name: string, value?: number ): void;
	// Optional census of admitted batches; absent during ordinary timing runs.
	characterBatch?( variant: string, actors: number, draws: number ): void;
	// World preparation stages and detail spans (renderer/world/world.ts).
	worldBegin?(): void;
	worldMark?( stage: string ): void;
	worldCount?( name: string, value: number ): void;
	sampleDetails?(): boolean;
	detailBegin?( name: string ): void;
	detailEnd?( name: string ): void;
}

/*
================
RuntimeDiagnostics

A read-only snapshot of runtime failures and bounded resource statistics.
================
*/
export interface RuntimeDiagnostics {
	readonly gpuAnimation?: boolean;
	readonly gpuTiming?: boolean;
	readonly hoverPicking?: boolean;
	readonly stages?: boolean;
}
/*
================
GpuTimingStats

Completed GPU timing samples; pending queries stay with the device owner.
================
*/
export interface GpuTimingStats {
	readonly attempted: number;
	readonly completed: number;
	readonly omittedPasses: number;
	readonly supported: boolean;
	readonly skipped: number;
	readonly failed: number;
	readonly samples: readonly {
		readonly sequence: number;
		readonly frameId?: number;
		readonly passes: readonly { readonly name: string; readonly ms: number; }[];
	}[];
}
/*
================
Disposable

The owner releases its resources explicitly at the end of its lifetime.
================
*/
export interface Disposable {
	dispose(): void;
}
/*
================
Viewport

Dimensions shared by surface allocation, projection and input mapping.
================
*/
export interface Viewport {
	width: number;
	height: number;
}
/** One published sample of the frame owner's own timing, for the FPS chip. */
export interface FrameTelemetry {
	readonly overload?: {
		readonly level: number;
		readonly longFrames: number;
		readonly recent: readonly { readonly atMs: number; readonly cpuMs: number; readonly level: number; }[];
	};
	readonly frameId?: number;
	readonly stages?: Readonly<Record<string, number>>;
	readonly gpu?: GpuTimingStats & { readonly enabled: boolean; };
	/** Frames per second averaged over the sampling window. */
	readonly fps: number;
	readonly pingMs?: number | null;
	/** Wall time between presented frames: window average and 95th percentile. */
	readonly frameMs: number;
	readonly p95FrameMs: number;
	/** Time spent inside the runtime frame callback: window average and 95th percentile. */
	readonly cpuMs: number;
	readonly p95CpuMs: number;
	readonly actors: number;
	readonly draws: number;
	readonly visibleGroups: number;
	/** The client and server builds, one line each when known, and their commit subjects (build-info.ts). */
	readonly build: { readonly lines: readonly string[]; readonly detail: string; };
}
/*
================
Platform

Browser services granted to the runtime by the platform boundary.
================
*/
export interface Platform extends Disposable {
	setMovementDump( dump: () => unknown ): void;
	setAssetDump( dump: () => unknown ): void;
	saveVideoOptions( value: import("@/engine/foundation/rendering/video-options").VideoOptions ): void;
	saveQuickslotOptions( value: import("@/engine/foundation/ui/extended-quickslot").ExtendedQuickslotOptions ): void;
	saveWindowPositions( value: import("@/engine/foundation/ui/window-positions").WindowPositions ): void;
	saveInputOptions( value: import("@/engine/foundation/ui/input-options").InputOptions ): void;
	saveSightMode( value: import("@/engine/foundation/rendering/camera-options").SightMode ): void;
	saveAudioOptions( value: import("@/engine/foundation/audio/options").AudioOptions ): void;
	saveChatBlocks( value: readonly string[] ): void;
	saveExperimentalOptions( value: import("@/engine/foundation/ui/experimental-options").ExperimentalOptions ): void;
	/** /cwsave's browser form: download the waypoint store's text as cw.txt (#1716). */
	saveCameraWaypoints( text: string ): void;
	saveGameOptions( value: import("@/engine/foundation/gameplay/game-options").GameOptions ): void;
	presentWorldCursor( cursor: import("@/engine/foundation/ui/world-cursor").WorldCursor ): void;
	setCursorVisible( visible: boolean ): void;
	readonly canvas: HTMLCanvasElement;
	presentLoading(
		state: { visible: boolean; title: string; progress: import("./assets").AssetProgress | null; }
	): void;
	presentUi( state: import("./ui").UiSemantics ): void;
	// Release skew (runtime/release/release-watch.ts): the entry bundle this page
	// loaded (null outside a release build), a one-shot "tab visible again"
	// trigger, and the refresh offer once a newer release is live.
	runningEntry(): string | null;
	visibilityReturned(): boolean;
	/** Consumes a browser online event independently of tab visibility. */
	connectionReturned(): boolean;
	presentUpdate( newer: boolean ): void;
	presentTelemetry( sample: FrameTelemetry ): void;
	diagnosticsActive(): boolean;
	/** Holds the tab awake (no background freeze) while a world session is live. */
	setKeepAwake( active: boolean ): void;
	/** The canvas CSS size, observed rather than read (no forced layout). */
	canvasSize(): { readonly width: number; readonly height: number; };
	readViewport(): Viewport;
	/** Logical UI extent; whole physical pixels per native bitmap pixel. */
	readUiViewport(): Viewport;
	/** CSS pixels per native UI pixel, shared by DOM controls and pointer input. */
	displayScale(): number;
	report( text: string, error?: unknown ): void;
}
/*
================
Renderer

The runtime grants scene and resource commands without exposing device ownership.
================
*/
export interface Renderer extends Disposable {
	setFrameWork?( work: FrameWork ): void;
	readbackWaitMs?(): number;
	scenery(): import("./scenery").SceneryPresentation | null;
	gpuTiming(): GpuTimingStats & { readonly enabled: boolean; };
	/** The current device's adapter facts for bug reports, null before one. */
	adapterFacts(): import("@/engine/foundation/rendering/gpu-facts").GpuFacts | null;
	videoOptions( value: import("@/engine/foundation/rendering/video-options").VideoOptions ): void;
	experimentalVideo( value: import("@/engine/foundation/ui/experimental-options").ExperimentalVideo ): void;
	// Port-only, not native: whether the Experimental enchant sparks are on.
	enchantSparks?(): boolean;
	// Port-only, not native: whether characters and effects share one
	// nearest-first actor room (Experimental; the original has no cap).
	sharedActorRoom?(): boolean;
	// Port-only, not native: whether holding V also hides other players'
	// companions (Experimental, off by default).
	vHidesCompanions?(): boolean;
	// Port-only, not native (Experimental "Dungeon character PVS", on by
	// default): whether dungeon character PVS and floor admission are on.
	dungeonCharacterPvs?(): boolean;
	// Port-only, not native: whether a character in dungeon room `block` is
	// hidden because the room the local player stands in (`activeBlock`, the
	// same frame's navigation block) does not draw it (Experimental
	// "Dungeon character PVS", on by default; off is the native frame).
	dungeonBlockHides?( block: number, activeBlock?: number ): boolean;
	setSelectionDecal(
		value: { readonly pose: import("./gameplay").Pose; readonly slot: 0 | 1 | 2 | 3; } | null
	): void;
	// One SWorld graphic option (foundation/rendering/graphic-options.ts)
	// as negated by the console's render family: the world renderer and
	// the character renderer each apply the options they own.
	setWorldRenderMode( mode: number, enabled: boolean ): void;
	setFootprints( value: readonly import("./footprint").Footprint[] ): void;
	// Port-only, not native (#1640): the ground rings under the player's
	// own admitted Seal of Sun drops (runtime/characters/sun-drop-markers.ts).
	setSunDropMarkers( value: readonly import("./sun-drop-marker").SunDropMarker[] ): void;
	pickGround( x: number, y: number ): import("./navigation").GroundPickQuery | null;
	presentationCamera(): import("@/engine/foundation/animation/entity-lod").LodPoint | null;
	audioListener(): import("./audio").SoundListener | null;
	pickDestination( x: number, y: number ): Pick<import("./gameplay").Pose, "regionId" | "x" | "y" | "z"> | null;
	pickFrontendCharacter( x: number, y: number, ids: readonly number[] ): number | null;
	pickFrontendRace( x: number, y: number ): 0 | 1 | null;
	frontendRaceCenters(): readonly (readonly [number, number, number] | null)[];
	setCharacterPreview( camera: import("./scene").WorldCamera | null ): void;
	/** Port-only: the doll target's last frame as packed RGBA (the 3D viewer's still). */
	readDoll?(): Promise<{ readonly width: number; readonly height: number; readonly rgba: Uint8ClampedArray; }>;
	/** Port-only: an actor's scaled bind-pose bounds [minX, minY, minZ, maxX, maxY, maxZ] (the 3D viewer's framing). */
	characterBounds?( gid: number ): readonly [number, number, number, number, number, number] | null;
	/** Port-only: the 3D viewer's studio light (key, fill and direction); null keeps the environment's. */
	setViewerLight?( light: ViewerLight | null ): void;
	characterParticleSnapshot( gid: number ): { readonly matrix: Float32Array; readonly regionId: number; } | null;
	characterParticleTime( gid: number ): number | undefined;
	presentationNight(): boolean;
	characterLocalMatrix(
		actors: readonly import("./character").CharacterActor[],
		gid: number,
		bone: string
	): Float32Array | null;
	characterMatrix( actors: readonly import("./character").CharacterActor[], gid: number ): Float32Array | null;
	characterSocket(
		actors: readonly import("./character").CharacterActor[],
		gid: number,
		bone: string | { name: string | null; fallback: "mount-root"; },
		/** A native model-space offset, turned by the holder's root (8D6880). */
		offset: readonly [number, number, number]
	): import("./character").CharacterActor["pose"] | null;
	pickEntity( x: number, y: number, excluded: number, blindHeld?: boolean, filtered?: boolean ): number | null;
	setWeather( value: import("@/engine/foundation/gameplay/weather").WeatherOptions | null ): void;
	setWorldClock( value: { timeOfDay: number; lunarDay: number; } | null ): void;
	setUi( scene: import("./ui").UiScene | null ): void;
	/** The damage text a scene's world annotations show this frame (UiScene.damageText). */
	setDamageText( rows: readonly import("./damage-text").DamageText[] ): void;
	setUiTexture( id: string, image: UiTexture | null ): void;
	// Port-only, not native: the icon atlas mapping (foundation/ui
	// /icon-atlas.ts) the interface's image owner installed.
	setUiAtlas( mapping: import("@/engine/foundation/ui/icon-atlas").IconAtlas | null ): void;
	retainCharacterModels( ids: readonly string[] ): void;
	setCharacterAssembly( id: string, base: string, parts: readonly import("./character").CharacterAttachment[] ): void;
	characterStats( details?: boolean, posed?: boolean ): CharacterStatistics;
	setCharacterModel( id: string, model: import("./character").CharacterModel, images: WorldTexture[] ): void;
	setCharacterAnimation(
		id: string,
		name: string,
		clip: import("@/engine/foundation/animation/native-clip").NativeClip
	): number;
	setTeleportGates( entities: readonly import("./world").EntityState[] ): void;
	setCharacterActors(
		actors: readonly import("./character").CharacterActor[],
		portraits?: readonly import("./character").CharacterActor[]
	): void;
	/** The actor snapshots the last setCharacterActors retained (read-only). */
	characterActors(): readonly import("./character").CharacterActor[];

	/** Copies the actual post-collision projection; no GPU readback or camera approximation. */
	worldView(): WorldViewSnapshot | null;
	setWorld( scene: import("./scene").WorldScene | null ): void;
	/** terrain: the outdoor region parts this scene composes with (world-admission.ts). */
	adoptWorld(
		world: import("./world-admission").WorldSceneLease,
		detail?: import("./scene").WorldScene["terrainDetail"],
		terrain?: readonly import("./world-admission").WorldTerrainPart[]
	): void;
	cancelWorldUpdate(): void;
	// Port-only: the texture compression features of the GPU device once it
	// exists, empty when it never does (foundation/assets/texture-variants.ts).
	textureFeatures(): Promise<readonly string[]>;
	// The drawn frontend scene sceneId gives up its CPU geometry; after a
	// device loss it is reported in worldStats().lostSceneId. False when it
	// is not the drawn scene or not at full terrain detail.
	settleWorld( sceneId: string ): boolean;
	setWorldCamera( camera: import("./scene").WorldCamera ): void;
	// The GM /colon byte (+0x3CC): false keeps the requested camera
	// distance (68FAD0's no-hit arm); the follow runs either way.
	setWorldCameraCollision( enabled: boolean ): void;
	setWorldTexture(
		path: string,
		image: import("./texture").WorldTexture,
		alpha?: import("@/engine/foundation/rendering/picking").PickAlpha
	): void;
	neededWorldTextures(): readonly string[];
	worldStats(): import("./scene").WorldRenderStats;
	setGeometryInstances( instances: Float32Array ): void;
	setGeometryTransform( transform: Float32Array ): void;
	setGeometry( data: Geometry | null ): void;
	setImage( image: ImageBitmap | null ): void;
	frame( viewport: Viewport, timeSeconds?: number, frameId?: number, probe?: RenderFrameProbe ): void | Promise<void>;
	phase(): RuntimePhase;
	error(): string | null;
}

/*
================
FrameWork

One runtime-owned optional budget is shared by all presentation consumers.
================
*/
export interface FrameWork {
	level(): number;
	remaining(): number;
	spend( ms: number ): void;
}
/*
================
SimulationHost

The simulation worker capability owns its message channel and lifecycle.
================
*/
export interface SimulationHost extends Disposable {
	pollWorld(): import("./world").WorldBatch | null;
	/** The transport's recent frame journal, oldest first (network.ts NET_JOURNAL_FIELDS per frame). */
	netJournal(): Float64Array;
	/** Resolves on the next worker message; paces frames while the tab is hidden. */
	delivery(): Promise<void>;
	ackWorld( sequence: number ): void;
	/** The input sequence stamped on a gameplay command (#1441a), else undefined. */
	session( command: SessionCommand ): number | undefined;
	pollSession(): SessionState | null;
	sendInput( batch: InputBatch ): void;
	poll(): SimulationObservation | null;
	error(): string | null;
}
/*
================
SimulationObservation

Read-only state published by the simulation to presentation owners.
================
*/
export interface SimulationObservation {
	clock?: ClockSample;
	publishedAtMs: number;
	appliedAtMs: number;
	acceptedInputSequence: number;
	sequence: number;
	timeMs: number;
	receivedAtMs: number;
}
/** Last completed worker wake; counters never include a partially executed step. */
export interface ClockSample {
	wakes: number;
	steps: number;
	stepsInWake: number;
	wakeMs: number;
	maxStepMs: number;
	debtMs: number;
	/** Epoch milliseconds (timeOrigin + now) of simulation time zero. Each step runs
	 * for a fixed deadline, so a simulation time maps to wall time by this origin
	 * even when a late wake executes several steps at once. */
	originMs: number;
}
/*
================
Clock

The frame owner supplies time through one visible runtime clock.
================
*/
export interface Clock extends Disposable {
	sample(): ClockSample;
	start(): void;
	reanchor?(): number;
}

/*
================
WorldViewSnapshot

Copied world projection used by picking, after camera collision and aspect
resolution. Coordinates are relative to originRegion; the matrix is column-major.
================
*/
export interface WorldViewSnapshot {
	readonly matrix: Float32Array;
	readonly originRegion: number;
	readonly width: number;
	readonly height: number;
}

/*
================
RuntimeControl

External controls enter through the runtime owner rather than mutating subsystems.
================
*/
export interface RuntimeControl extends Disposable {
	audioSnapshot(): import("./audio").AudioResidencySnapshot;
	/** Bounded copied diagnostics; collecting them never waits for GPU readback. */
	gpuTiming(): GpuTimingStats & { readonly enabled: boolean; };
	retryWorld(): void;
	session( command: SessionCommand ): void;
	sessionState(): SessionState | null;
	entity( gid: number ): import("./world").EntityState | undefined;
	gameplay(): import("./gameplay").GameplayState | null;
	/** The presented entities, as the UI receives them each frame. */
	entities(): readonly import("./world").EntityState[];
	/** The Berserk orb gauge the UI presents. */
	berserkGauge(): import("./orb").BerserkGauge | undefined;
	/** The actors the renderer draws this frame. */
	characterActors(): readonly import("./character").CharacterActor[];
	/** Copied counters and retained-pose eligibility; does not advance animation. */
	characterStats(): CharacterStatistics;
	/**
	 * characterStats plus the posed culling ceiling. Reads displayed-pose
	 * palettes, which can force CPU pose evaluation: diagnostics only, never
	 * inside a measured frame window.
	 */
	characterCullCensus(): CharacterStatistics;
	/** Read-only orbit camera the input owner holds (yaw/pitch/distance). */
	camera(): import("./input").CameraInput;
	/** Copied last prepared world view; null before preparation or after failure/disposal. */
	worldView(): WorldViewSnapshot | null;
	takeNative(): import("./world").WorldEvent[];
}

/*
================
ViewerLight

Port-only, not native: the 3D viewer's studio light. The renderer writes it
over the frame environment's diffuse and ambient colours (linear 0..1 RGB)
and lights along direction (a unit vector toward the light, in the doll's
space) in place of the retail 45-degree light. The game never sets one, so
every in-game portrait keeps the native light.
================
*/
export interface ViewerLight {
	readonly diffuse: readonly [number, number, number];
	readonly ambient: readonly [number, number, number];
	readonly direction: readonly [number, number, number];
}
