/*
===========================================================================

audio.ts - sound events, listeners and read-only residency diagnostics

===========================================================================
*/
/*
================
ItemSoundRequest
================
*/
export interface ItemSoundRequest {
	readonly handle: "SND_EQUIP" | "SND_DROPITEM";
	readonly typeFlags: number;
}

/*
================
SoundEvent
================
*/
export interface SoundEvent {
	readonly loop?: boolean;
	readonly stop?: boolean;
	readonly spatial?: boolean;
	readonly id: string;
	readonly path: string;
	readonly gain: number;
	readonly x: number;
	readonly y: number;
	readonly z: number;
	readonly expires: number;
}

/*
================
SoundListener
================
*/
export interface SoundListener {
	readonly position: readonly [number, number, number];
	readonly forward: readonly [number, number, number];
	readonly up: readonly [number, number, number];
}

/*
================
AudioResidencySnapshot

Detached cache accounting. Diagnostics never retain Web Audio buffers.
================
*/
export interface AudioResidencySnapshot {
	readonly sampleRate: number;
	readonly limitBytes: number;
	readonly residentBytes: number;
	readonly admitted: boolean;
	readonly required: readonly string[];
	readonly decoding: readonly string[];
	readonly buffers: readonly { path: string; bytes: number; playing: boolean; preparing: boolean; ui: boolean; }[];
}
