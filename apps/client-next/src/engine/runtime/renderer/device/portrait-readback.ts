/*
===========================================================================

portrait-readback.ts - a portrait target's pixels copied back to the CPU

Port-only, not native: the 3D viewer's still (docs/guides/VIEWER.md). One copy
per call, submitted after the frames already queued so it reads what they
drew, then unpacked to 8-bit RGBA (texel-readback.ts). The staging buffer
retires through the device's retirement, never destroyed directly. The
game never calls it.

===========================================================================
*/
import { paddedRowBytes, unpackRgba8 } from "@/engine/foundation/rendering/texel-readback";
import type { Retire } from "./retirement";

/*
================
PortraitPixels
================
*/
export interface PortraitPixels {
	readonly width: number;
	readonly height: number;
	readonly rgba: Uint8ClampedArray;
}

/*
================
PortraitSource

The target texture and what the copy needs to know about it.
================
*/
export interface PortraitSource {
	readonly label: string;
	readonly texture: GPUTexture;
	readonly width: number;
	readonly height: number;
	readonly format: GPUTextureFormat;
}

/*
================
readPortraitPixels
================
*/
export async function readPortraitPixels(
	device: GPUDevice,
	source: PortraitSource,
	retire: Retire
): Promise<PortraitPixels> {
	const { width, height } = source, stride = paddedRowBytes( width, source.format );
	const buffer = device.createBuffer( {
		label: source.label + "-readback",
		size: stride * height,
		usage: GPUBufferUsage.COPY_DST | GPUBufferUsage.MAP_READ
	} );
	try {
		const encoder = device.createCommandEncoder( { label: source.label + "-readback" } );
		encoder.copyTextureToBuffer( { texture: source.texture }, { buffer, bytesPerRow: stride }, [ width, height ] );
		device.queue.submit( [ encoder.finish() ] );
		await buffer.mapAsync( GPUMapMode.READ );
		const rgba = unpackRgba8( new Uint8Array( buffer.getMappedRange() ), width, height, source.format );
		buffer.unmap();
		return { width, height, rgba };
	} finally {
		retire( buffer );
	}
}
