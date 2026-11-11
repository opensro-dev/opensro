/*
===========================================================================

gpu-facts.ts - what the player's WebGPU adapter offers, for a report

A phone that crashes on memory may lack the compressed texture formats the
client uses (BC) and fall back to uncompressed RGBA8; whether it offers
ETC2 or ASTC instead decides the fix. A report therefore carries the
adapter's texture-compression and related features, the limits that bound
the client's buffers, and its vendor and architecture tokens: enums and
numbers only (bug-report archive.ts admits nothing else). The device owner
reads them (gpuFacts in renderer/device/device.ts); this is only their shape.

===========================================================================
*/

// GPU_TOKEN_PATTERN is the shape of an adapter's vendor or architecture
// token ("apple", "common-3", "gen-12lp"), at most 32 characters; the bug
// report's archive builds its allowlist pattern from the same source.
export const GPU_TOKEN_PATTERN = "^[a-z0-9][a-z0-9-]{0,31}$";

/*
================
reportedGpuFeatures

The adapter features a report names when present: the compressed texture
families and the few capabilities the renderer branches on.
================
*/
export function reportedGpuFeatures(): readonly string[] {
	return [
		"texture-compression-bc",
		"texture-compression-bc-sliced-3d",
		"texture-compression-etc2",
		"texture-compression-astc",
		"texture-compression-astc-sliced-3d",
		"timestamp-query",
		"shader-f16",
		"float32-filterable"
	];
}

/*
================
reportedDegradedPipelines

Optional compute pipelines that degrade to CPU or no-op paths when a driver
fails to compile them.
================
*/
export function reportedDegradedPipelines(): readonly string[] {
	return [
		"skeletal-animation",
		"particle-presentation",
		"flare-visibility"
	];
}

/*
================
adapterRoutesBitmapsViaPixels

Whether UI bitmaps reach the GPU as read-back pixels instead of an
external-image copy on this adapter. The Kepler-generation NVIDIA
sessions of #3147 came up with the copied bitmaps broken - the font
atlas read as rows of dots and large window plates transparent -
while every byte-uploaded texture on the same device (the world, the
native UI tiles) rendered. The pixel route is the byte upload those
textures took; every other adapter keeps the copy.
================
*/
export function adapterRoutesBitmapsViaPixels( facts: GpuFacts ): boolean {
	return facts.vendor === "nvidia" && facts.architecture === "kepler";
}

/*
================
isGpuToken

Whether value has the shape of an adapter's vendor or architecture token
("apple", "common-3", "gen-12lp"): lowercase letters, digits and hyphens.
Anything else may be free text and is left out.
================
*/
export function isGpuToken( value: string ): boolean {
	return new RegExp( GPU_TOKEN_PATTERN ).test( value );
}

/*
================
GpuFacts
================
*/
export interface GpuFacts {
	readonly features: readonly string[];
	readonly limits: {
		readonly maxBufferSize: number;
		readonly maxStorageBufferBindingSize: number;
		readonly maxTextureDimension2D: number;
	};
	readonly vendor?: string;
	readonly architecture?: string;
	readonly degradedPipelines?: readonly string[];
}
