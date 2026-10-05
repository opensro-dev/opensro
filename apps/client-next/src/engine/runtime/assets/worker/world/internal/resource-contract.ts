/*
===========================================================================

resource-contract.ts - the published world bundle as the decoder reads it

Types only: the region bundle layout the asset pipeline writes and the
world decoder admits.

===========================================================================
*/

import type { WorldObjectMaterialSource } from "@/engine/foundation/rendering/world-material";
export interface ObjectBranch {
	modifiers?: import("@/engine/foundation/rendering/scenery-modifiers").SceneryModifiers;
	sourcePath?: string;
	materialPaths: string[];
	meshPaths: string[];
	renderMeshSection?: { paths: string[]; };
}
export interface Mesh {
	sourcePath: string;
	headerOffsets?: number[];
	metadata: { materialName: string; };
	positions: number[];
	normals: number[];
	uvs: number[];
	indices: number[];
	bounds: { min: number[]; max: number[]; };
}
export interface Material extends WorldObjectMaterialSource {
	name: string;
}
interface Block {
	blockX: number;
	blockZ: number;
	heights: number[];
	textureData: number[];
	water: { type: number; waveType: number; height: number; };
}
interface Sector {
	lightmapPublicPath?: string;
	sectorX: number;
	sectorY: number;
	blocks: Block[];
}
export interface AnimatedResource {
	sourcePath: string;
	glbPublicPath: string;
	clipName: string;
	skinnedMeshPaths: string[];
	model?: import("@/engine/contracts/character").CharacterModel;
}
export interface Bundle {
	navmesh?: { regions: { dx: number; dz: number; tileTextureIds?: string; }[]; };
	dungeonBlocks?: {
		index: number;
		visibleBlocks: number[];
		fog: NonNullable<import("@/engine/contracts/scene").WorldMaterial["fog"]>;
	}[];
	dungeonWater?: import("@/engine/foundation/rendering/dungeon-water").DungeonWaterSurface[];
	animated?: AnimatedResource[];
	sky?: {
		flareTexturePublicPaths?: string[];
		cloudTexturePublicPath?: string;
		sunTexturePublicPath?: string;
		moonTexturePublicPaths?: string[];
		starPrimitive?: {
			nativeRand?: { stateAfterConstruction?: number; };
			vertices: { x: number; y: number; z: number; colorArgb: number; }[];
		};
		environment?: import("@/engine/contracts/scene").WorldEnvironment;
	};
	source: { sectorX: number; sectorY: number; };
	terrain: { sectors?: Sector[]; blocks: Block[]; };
	terrainTextures: {
		sectors?: { sectorX: number; sectorY: number; lightmapPublicPath?: string; }[];
		lightmapPublicPath?: string;
		tileCatalog: { referencedTiles: { textureId: number; flags?: number; imagePublicPath: string; }[]; };
	};
	water?: { specialTexturePublicPath?: string; normalFramePublicPaths: string[]; };
	objects: {
		placements: {
			dungeonBlock?: number;
			lodGroupIndex?: number;
			blockX?: number;
			blockZ?: number;
			sourceSector?: { sectorId: string; };
			objectId: number;
			uid: number;
			regionId: string;
			position: { x: number; y: number; z: number; };
			yaw: number;
		}[];
		resources: {
			meshes: Mesh[];
			bsr: (ObjectBranch & { objectId: number; branches?: ObjectBranch[]; })[];
			materialSets: { sourcePath: string; materials: Material[]; }[];
		};
	};
}
