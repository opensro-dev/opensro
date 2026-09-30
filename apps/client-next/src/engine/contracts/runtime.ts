/*
===========================================================================

runtime.ts - the runtime contracts

The interfaces the runtime owners implement and call across: the platform
(page, input, preferences, overlays), the renderer, and the runtime control
surface. Types only.

===========================================================================
*/

import type { Geometry } from "./geometry";
import type { SessionCommand, SessionState } from "./session";
import type { InputBatch } from "./input";
/*
================
RuntimePhase
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
}

/*
================
RuntimeDiagnostics
================
*/
export interface RuntimeDiagnostics {
	readonly animationPose?: import("@/engine/foundation/animation/animation-pose").AnimationPoseProbe;
	readonly gpuAnimation?: boolean;
	readonly gpuTiming?: boolean;
	readonly hoverPicking?: boolean;
	readonly stages?: boolean;
}
/*
================
GpuTimingStats
================
*/
export interface GpuTimingStats {
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
================
*/
export interface Disposable {
	dispose(): void;
}
/*
================
Viewport
================
*/
export interface Viewport {
	width: number;
	height: number;
}
/** One published sample of the frame owner's own timing, for the FPS chip. */
export interface FrameTelemetry {
	readonly frameId?: number;
	readonly stages?: Readonly<Record<string, number>>;
	readonly gpu?: GpuTimingStats & { readonly enabled: boolean; };
	/** Frames per second averaged over the sampling window. */
	readonly fps: number;
	/** Wall time between presented frames: window average and 95th percentile. */
	readonly frameMs: number;
	readonly p95FrameMs: number;
	/** Time spent inside the runtime frame callback: window average and 95th percentile. */
	readonly cpuMs: number;
	readonly p95CpuMs: number;
	readonly actors: number;
	readonly draws: number;
	readonly visibleGroups: number;
}
/*
================
Platform
================
*/
export interface Platform extends Disposable {
	saveVideoOptions( value: import("@/engine/foundation/rendering/video-options").VideoOptions ): void;
	saveQuickslotOptions( value: import("@/engine/foundation/ui/extended-quickslot").ExtendedQuickslotOptions ): void;
	saveInputOptions( value: import("@/engine/foundation/ui/input-options").InputOptions ): void;
	saveSightMode( value: import("@/engine/foundation/rendering/camera-options").SightMode ): void;
	saveAudioOptions( value: import("@/engine/foundation/audio/options").AudioOptions ): void;
	saveChatBlocks( value: readonly string[] ): void;
	saveGameOptions( value: import("@/engine/foundation/gameplay/game-options").GameOptions ): void;
	presentWorldCursor( cursor: import("@/engine/foundation/ui/world-cursor").WorldCursor ): void;
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
	presentUpdate( newer: boolean ): void;
	presentTelemetry( sample: FrameTelemetry ): void;
	readViewport(): Viewport;
	report( text: string, error?: unknown ): void;
}
/*
================
Renderer
================
*/
export interface Renderer extends Disposable {
	scenery(): import("./scenery").SceneryPresentation | null;
	gpuTiming(): GpuTimingStats & { readonly enabled: boolean; };
	videoOptions( value: import("@/engine/foundation/rendering/video-options").VideoOptions ): void;
	setSelectionDecal(
		value: { readonly pose: import("./gameplay").Pose; readonly slot: 0 | 1 | 2 | 3; } | null
	): void;
	setFootprints( value: readonly import("./footprint").Footprint[] ): void;
	pickGround( x: number, y: number ): import("./navigation").GroundPickQuery | null;
	presentationCamera(): import("@/engine/foundation/animation/entity-lod").LodPoint | null;
	audioListener(): import("./audio").SoundListener | null;
	pickDestination( x: number, y: number ): Pick<import("./gameplay").Pose, "regionId" | "x" | "y" | "z"> | null;
	pickFrontendCharacter( x: number, y: number, ids: readonly number[] ): number | null;
	pickFrontendRace( x: number, y: number ): 0 | 1 | null;
	frontendRaceCenters(): readonly (readonly [number, number, number] | null)[];
	setCharacterPreview( camera: import("./scene").WorldCamera | null ): void;
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
		bone: string,
		offset: readonly [number, number, number]
	): import("./character").CharacterActor["pose"] | null;
	pickEntity( x: number, y: number, excluded: number, blindHeld?: boolean ): number | null;
	setWeather( value: import("@/engine/foundation/gameplay/weather").WeatherOptions | null ): void;
	setWorldClock( value: { timeOfDay: number; lunarDay: number; } | null ): void;
	setUi( scene: import("./ui").UiScene | null ): void;
	setUiTexture( id: string, image: ImageBitmap | ImageData | null ): void;
	retainCharacterModels( ids: readonly string[] ): void;
	setCharacterAssembly( id: string, base: string, parts: readonly import("./character").CharacterAttachment[] ): void;
	characterStats(): { actors: number; draws: number; };
	setCharacterModel( id: string, model: import("./character").CharacterModel, images: ImageBitmap[] ): void;
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

	setWorld( scene: import("./scene").WorldScene | null ): void;
	adoptWorld(
		world: import("./world-admission").WorldSceneLease,
		detail?: import("./scene").WorldScene["terrainDetail"]
	): void;
	cancelWorldUpdate(): void;
	setWorldCamera( camera: import("./scene").WorldCamera ): void;
	setWorldTexture( path: string, image: import("./texture").WorldTexture ): void;
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
SimulationHost
================
*/
export interface SimulationHost extends Disposable {
	pollWorld(): import("./world").WorldBatch | null;
	ackWorld( sequence: number ): void;
	session( command: SessionCommand ): void;
	pollSession(): SessionState | null;
	sendInput( batch: InputBatch ): void;
	poll(): SimulationObservation | null;
	error(): string | null;
}
/*
================
SimulationObservation
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
}
/*
================
Clock
================
*/
export interface Clock extends Disposable {
	sample(): ClockSample;
	start(): void;
}

/*
================
RuntimeControl
================
*/
export interface RuntimeControl extends Disposable {
	audioSnapshot(): import("./audio").AudioResidencySnapshot;
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
	/** Read-only orbit camera the input owner holds (yaw/pitch/distance). */
	camera(): import("./input").CameraInput;
	takeNative(): import("./world").WorldEvent[];
}
