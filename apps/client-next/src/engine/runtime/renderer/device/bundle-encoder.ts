/*
===========================================================================

bundle-encoder.ts - a render bundle encoder that can only be finished once

Recording forwards to the raw encoder created by the device until
finish() hands out the bundle; from there every recording call - and
finish() itself - is refused here, in the frame's own terms, instead of
surfacing later as a backend validation error about a finished encoder
(#2560). The wrapper exists because a WebGPU bundle encoder never
reopens: single use is a construction fact, not a caller convention.
The raw encoder type comes from the frame contract, like every other
device module's GPU surface.

===========================================================================
*/
import type { FrameCommands } from "@/engine/runtime/renderer/internal/gpu-contract";

// RawBundleEncoder is the contract's bundle encoder, unnamed here on
// purpose: the raw encoder types belong to the frame capability.
type RawBundleEncoder = ReturnType<FrameCommands["createBundleEncoder"]>;

/*
================
createSingleUseBundleEncoder

Wraps one raw bundle encoder. The finished flag is set before finish()
delegates, so even a finish that throws leaves the encoder closed.
================
*/
export function createSingleUseBundleEncoder( encoder: RawBundleEncoder ): RawBundleEncoder {
	let finished = false;

	/*
	================
	open

	The raw encoder while it can still record; refusal after finish.
	================
	*/
	const open = (): RawBundleEncoder => {
		if ( finished ) {
			throw Error( "Render bundle encoder used after finish" );
		}
		return encoder;
	};

	/*
	================
	setBindGroup

	The interface's two call shapes over the shared forwarder below.
	================
	*/
	function setBindGroup(
		index: number,
		bindGroup: GPUBindGroup | null,
		dynamicOffsets?: Iterable<number>
	): undefined;
	function setBindGroup(
		index: number,
		bindGroup: GPUBindGroup | null,
		dynamicOffsetsData: Uint32Array,
		dynamicOffsetsDataStart: number,
		dynamicOffsetsDataLength: number
	): undefined;
	function setBindGroup( ...args: unknown[] ): void {
		open().setBindGroup( ...(args as Parameters<RawBundleEncoder["setBindGroup"]>) );
	}

	return {
		/*
		================
		label
		================
		*/
		get label() {
			return encoder.label;
		},

		/*
		================
		label
		================
		*/
		set label( value: string ) {
			encoder.label = value;
		},

		/*
		================
		__brand

		The WebGPU typings' nominal brand: carried so this wrapper is
		the interface it wraps. Nothing reads it at runtime.
		================
		*/
		__brand: "GPURenderBundleEncoder",

		setBindGroup,

		/*
		================
		setImmediates
		================
		*/
		setImmediates( ...args: Parameters<RawBundleEncoder["setImmediates"]> ) {
			open().setImmediates( ...args );
		},

		/*
		================
		setPipeline
		================
		*/
		setPipeline( ...args: Parameters<RawBundleEncoder["setPipeline"]> ) {
			open().setPipeline( ...args );
		},

		/*
		================
		setVertexBuffer
		================
		*/
		setVertexBuffer( ...args: Parameters<RawBundleEncoder["setVertexBuffer"]> ) {
			open().setVertexBuffer( ...args );
		},

		/*
		================
		setIndexBuffer
		================
		*/
		setIndexBuffer( ...args: Parameters<RawBundleEncoder["setIndexBuffer"]> ) {
			open().setIndexBuffer( ...args );
		},

		/*
		================
		draw
		================
		*/
		draw( ...args: Parameters<RawBundleEncoder["draw"]> ) {
			open().draw( ...args );
		},

		/*
		================
		drawIndexed
		================
		*/
		drawIndexed( ...args: Parameters<RawBundleEncoder["drawIndexed"]> ) {
			open().drawIndexed( ...args );
		},

		/*
		================
		drawIndirect
		================
		*/
		drawIndirect( ...args: Parameters<RawBundleEncoder["drawIndirect"]> ) {
			open().drawIndirect( ...args );
		},

		/*
		================
		drawIndexedIndirect
		================
		*/
		drawIndexedIndirect( ...args: Parameters<RawBundleEncoder["drawIndexedIndirect"]> ) {
			open().drawIndexedIndirect( ...args );
		},

		/*
		================
		insertDebugMarker
		================
		*/
		insertDebugMarker( ...args: Parameters<RawBundleEncoder["insertDebugMarker"]> ) {
			open().insertDebugMarker( ...args );
		},

		/*
		================
		pushDebugGroup
		================
		*/
		pushDebugGroup( ...args: Parameters<RawBundleEncoder["pushDebugGroup"]> ) {
			open().pushDebugGroup( ...args );
		},

		/*
		================
		popDebugGroup
		================
		*/
		popDebugGroup( ...args: Parameters<RawBundleEncoder["popDebugGroup"]> ) {
			open().popDebugGroup( ...args );
		},

		/*
		================
		finish
		================
		*/
		finish( ...args: Parameters<RawBundleEncoder["finish"]> ): ReturnType<RawBundleEncoder["finish"]> {
			const raw = open();
			finished = true;
			return raw.finish( ...args );
		}
	};
}
