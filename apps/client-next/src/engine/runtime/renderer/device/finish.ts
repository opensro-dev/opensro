/*
===========================================================================

finish.ts - the presentation pass: FXAA, adaptive sharpen, vibrance, contrast

The native client blits the composed offscreen frame to the swapchain
untouched (a copyTextureToTexture in device.ts). When the renderer enables
this owner, the copy becomes one fullscreen triangle that samples the
offscreen frame and writes the swapchain:

- FXAA 3.11 resolves the aliasing the 2005 rasterizer baked into geometry
  edges without touching the 46-pipeline matrix. Near-binary edges
  (typography, UI borders) are detected and passed through untouched -
  the UI shares this frame, and a glyph stem does not survive a
  one-texel resample,
- a gradient-limited unsharp mask sharpens the soft 2005 texture look
  without ringing halos on hard edges,
- a vibrance lift and a smoothstep S-curve grade the washed-out LDR
  palette while keeping black, mid-gray and white fixed, so flat colours
  and UI panels survive the pass byte-for-byte in practice.

Anti-alias first, sharpen second: the gradient limiter reads the raw
neighbourhood, so the unsharp mask stays off exactly the edges FXAA just
smoothed and the two stages do not fight.

The constants below are the whole tuning surface; nothing else in the
frame changes. device.ts owns this module and calls present() from
ColorTarget.present(), replacing the plain copy.

===========================================================================
*/

// FXAA 3.11 thresholds (Timothy Lottes), preset-12 values: edges below
// either floor are left alone, which is what keeps flat interiors and UI
// panels byte-exact through the pass.
const FXAA_MIN_LUMA = 0.0625; // absolute local-contrast floor (1/16)
const FXAA_EDGE_THRESHOLD = 0.125; // relative-to-max-luma floor (1/8)
const FXAA_ITERATIONS = 12; // edge-walk refinement steps
const FXAA_SUBPIXEL = 0.25; // sub-texel blend on soft detail (quality preset; the console 0.75 reads as smoothed)
// The UI is composed into the offscreen frame before this pass runs, so
// FXAA sees typography. Glyph edges are near-binary (a full-tone jump in
// one texel); geometry edges never are. Anything whose 3x3 luma range
// exceeds this floor passes through untouched - the pass shipped without
// the guard and the typography lost its edge, which is how this value
// earned its place.
const FXAA_HARD_EDGE = 0.5; // sqrt-luma range of a near-binary edge (text, UI borders)

// Grade constants tuned against the v1.150 LDR palette: strong enough to
// read as modern, weak enough never to crush the authored sky gradients.
const SHARPEN_AMOUNT = 0.45; // detail gain applied to the 4-neighbour blur residual
const SHARPEN_RANGE = 1.5; // local gradient that fully disables sharpening (halo guard)
const VIBRANCE = 0.14; // saturation lift around luma
const CONTRAST = 0.22; // smoothstep S-curve blend weight, endpoints preserved

// Output dither: the S-curve stretches the palette, and an 8-bit swapchain
// bands where it does. A triangular white-noise offset kills the banding;
// gating it on the grade's local delta keeps the fixed points byte-exact.
const DITHER_STEP = 1 / 255; // one half-step of triangular-PDF noise
const DITHER_GATE = 8; // grade delta (1/8 = 0.125) that fully opens the gate

export interface FinishPresent {
	readonly ready: Promise<void>;
	present( source: GPUTexture, target: GPUTexture ): void;
	dispose(): void;
}

/*
================
createFinish

Build the presentation pipeline for the browser's preferred canvas format.
Mirrors bloom.ts: compilation is checked before the pipeline is created,
and readiness gates the device's running phase.
================
*/
export function createFinish( created: GPUDevice, format: GPUTextureFormat ): FinishPresent {
	const module = created.createShaderModule( {
		label: "presentation-finish",
		code: `
@group(0) @binding(0) var source:texture_2d<f32>;
@group(0) @binding(1) var linear:sampler;
struct V {@builtin(position) position:vec4f,@location(0) uv:vec2f}
@vertex fn vs(@builtin(vertex_index) i:u32)->V{
 let p=array<vec2f,3>(vec2f(-1,-1),vec2f(3,-1),vec2f(-1,3))[i];
 return V(vec4f(p,0,1),vec2f(p.x*.5+.5,.5-p.y*.5));
}
fn tap(uv:vec2f)->vec3f{return textureSampleLevel(source,linear,uv,0).rgb;}
// FXAA works on perceptual luma (sqrt of the rec.609 weight) per 3.11.
fn lumaOf(c:vec3f)->f32{return sqrt(dot(c,vec3f(0.299,0.587,0.114)));}
fn graded(c:vec3f)->vec3f{
 let luma=dot(c,vec3f(0.299,0.587,0.114));
 // Vibrance first, clamped so the S-curve below stays monotone on [0,1].
 let vibrant=clamp(mix(vec3f(luma),c,1.0+${VIBRANCE}),vec3f(0),vec3f(1));
 let curved=vibrant*vibrant*(vec3f(3.0)-2.0*vibrant);
 return clamp(mix(vibrant,curved,${CONTRAST}),vec3f(0),vec3f(1));
}
fn noise(uv:vec2f,seed:vec2f)->f32{
 return fract(sin(dot(uv+seed,vec2f(12.9898,78.233)))*43758.5453);
}
// FXAA 3.11 quality edge walk, the control flow as cleaned by Rendu/Fyrox:
// detect the edge axis from 3x3 luma, walk both ways along it until the
// luma deltas clear the local gradient, then resample half a texel toward
// the pixel's own side of the edge. Flat texels return untouched.
fn walkQuality(i:i32)->f32{
 if(i<5){return 1.0;}if(i==5){return 1.5;}if(i<10){return 2.0;}if(i==10){return 4.0;}return 8.0;
}
fn fxaa(uv:vec2f,offset:vec2f)->vec3f{
 let center=tap(uv);
 let lumaM=lumaOf(center);
 let lumaD=lumaOf(tap(uv+vec2f(0,-offset.y)));
 let lumaU=lumaOf(tap(uv+vec2f(0,offset.y)));
 let lumaL=lumaOf(tap(uv+vec2f(-offset.x,0)));
 let lumaR=lumaOf(tap(uv+vec2f(offset.x,0)));
 let lo=min(lumaM,min(min(lumaD,lumaU),min(lumaL,lumaR)));
 let hi=max(lumaM,max(max(lumaD,lumaU),max(lumaL,lumaR)));
 if(hi-lo<max(${FXAA_MIN_LUMA},${FXAA_EDGE_THRESHOLD}*hi)){return center;}
 // Near-binary edges are typography and UI borders, not geometry: leave
 // them exactly as rasterized. FXAA's one-texel resample is what a glyph
 // stem cannot survive.
 if(hi-lo>${FXAA_HARD_EDGE}){return center;}
 let lumaDL=lumaOf(tap(uv+vec2f(-offset.x,-offset.y)));
 let lumaUR=lumaOf(tap(uv+vec2f(offset.x,offset.y)));
 let lumaUL=lumaOf(tap(uv+vec2f(-offset.x,offset.y)));
 let lumaDR=lumaOf(tap(uv+vec2f(offset.x,-offset.y)));
 let lumaDU=lumaD+lumaU;let lumaLR=lumaL+lumaR;
 let lumaLC=lumaDL+lumaUL;let lumaDC=lumaDL+lumaDR;let lumaRC=lumaDR+lumaUR;let lumaUC=lumaUR+lumaUL;
 // The edge axis: the side whose cross-column luma sums swing least.
 let edgeH=abs(-2.0*lumaL+lumaLC)+abs(-2.0*lumaM+lumaDU)*2.0+abs(-2.0*lumaR+lumaRC);
 let edgeV=abs(-2.0*lumaU+lumaUC)+abs(-2.0*lumaM+lumaLR)*2.0+abs(-2.0*lumaD+lumaDC);
 let horizontal=edgeH>=edgeV;
 var step=select(offset.x,offset.y,horizontal);
 var luma1=select(lumaL,lumaD,horizontal);
 var luma2=select(lumaR,lumaU,horizontal);
 let gradient1=luma1-lumaM;let gradient2=luma2-lumaM;
 let steepest=abs(gradient1)>=abs(gradient2);
 let gradientScaled=0.25*max(abs(gradient1),abs(gradient2));
 var lumaAverage=0.0;
 if(steepest){step=-step;lumaAverage=0.5*(luma1+lumaM);}else{lumaAverage=0.5*(luma2+lumaM);}
 // Probe from half a texel inside the bright side of the edge, walking
 // along the edge itself to find where it ends both ways.
 var probeUv=uv;var walk=vec2f(0);
 if(horizontal){probeUv.y=probeUv.y+step*0.5;walk.x=offset.x;}else{probeUv.x=probeUv.x+step*0.5;walk.y=offset.y;}
 var uv1=probeUv-walk;var uv2=probeUv+walk;
 var lumaEnd1=lumaOf(tap(uv1))-lumaAverage;var lumaEnd2=lumaOf(tap(uv2))-lumaAverage;
 var reached1=abs(lumaEnd1)>=gradientScaled;var reached2=abs(lumaEnd2)>=gradientScaled;
 uv1=select(uv1-walk,uv1,reached1);
 uv2=select(uv2+walk,uv2,reached2);
 if(!(reached1&&reached2)){
  for(var i:i32=2;i<${FXAA_ITERATIONS};i=i+1){
   if(!reached1){lumaEnd1=lumaOf(tap(uv1))-lumaAverage;}
   if(!reached2){lumaEnd2=lumaOf(tap(uv2))-lumaAverage;}
   reached1=abs(lumaEnd1)>=gradientScaled;reached2=abs(lumaEnd2)>=gradientScaled;
   if(!reached1){uv1=uv1-walk*walkQuality(i);}
   if(!reached2){uv2=uv2+walk*walkQuality(i);}
   if(reached1&&reached2){break;}
  }
 }
 let distance1=select(uv.y-uv1.y,uv.x-uv1.x,horizontal);
 let distance2=select(uv2.y-uv.y,uv2.x-uv.x,horizontal);
 let closer=distance1<distance2;
 let edgeThickness=distance1+distance2;
 let centerSmaller=lumaM<lumaAverage;
 let variation1=(lumaEnd1<0.0)!=centerSmaller;
 let variation2=(lumaEnd2<0.0)!=centerSmaller;
 let correct=select(variation2,variation1,closer);
 let pixelOffset=-min(distance1,distance2)/edgeThickness+0.5;
 var finalOffset=select(0.0,pixelOffset,correct);
 // Sub-texel term: on detail softer than the edge walk resolves, blend
 // toward the 3x3 luma average so thin features do not shimmer.
 let luma3x3=(1.0/12.0)*(2.0*(lumaDU+lumaLR)+lumaLC+lumaRC);
 let subpixel1=clamp(abs(luma3x3-lumaM)/(hi-lo),0.0,1.0);
 let subpixel2=(-2.0*subpixel1+3.0)*subpixel1*subpixel1;
 finalOffset=max(finalOffset,subpixel2*subpixel2*${FXAA_SUBPIXEL});
 var finalUv=uv;
 if(horizontal){finalUv.y=finalUv.y+finalOffset*step;}else{finalUv.x=finalUv.x+finalOffset*step;}
 return tap(finalUv);
}
@fragment fn fs(v:V)->@location(0) vec4f{
 let offset=vec2f(1.0)/vec2f(textureDimensions(source));
 // Resolve the edge first; the sharpen below reads the raw neighbourhood
 // so its halo guard silences it exactly where FXAA just smoothed.
 let center=fxaa(v.uv,offset);
 let north=tap(v.uv+vec2f(0,-offset.y));
 let south=tap(v.uv+vec2f(0,offset.y));
 let west=tap(v.uv+vec2f(-offset.x,0));
 let east=tap(v.uv+vec2f(offset.x,0));
 // Gradient-limited unsharp mask: flat texels gain the full detail term,
 // busy edges none, so the mask cannot overshoot into halos.
 let gradient=max(abs(center-north)+abs(center-south),abs(center-west)+abs(center-east));
 let strength=${SHARPEN_AMOUNT}*clamp(vec3f(1.0)-${SHARPEN_RANGE}*gradient,vec3f(0.0),vec3f(1.0));
 let blur=(north+south+west+east)*0.25;
 let sharpened=clamp(center+(center-blur)*strength,vec3f(0),vec3f(1));
 let gradedColor=graded(sharpened);
 // Dither only where the grade moved the value: the gate is closed at the
 // fixed points (delta 0), fully open a quarter-tone away, which is also
 // where the curve is steepest and the banding would be.
 let gate=clamp(abs(gradedColor-sharpened)*${DITHER_GATE},vec3f(0),vec3f(1));
 let dither=(noise(v.uv,vec2f(0,0))+noise(v.uv,vec2f(7.13,3.71))-1.0)*${DITHER_STEP};
 return vec4f(gradedColor+vec3f(dither)*gate,1.0);
}`
	} );
	const sampler = created.createSampler( {
		minFilter: "linear",
		magFilter: "linear",
		addressModeU: "clamp-to-edge",
		addressModeV: "clamp-to-edge"
	} );
	const layout = created.createBindGroupLayout( {
		entries: [
			{ binding: 0, visibility: GPUShaderStage.FRAGMENT, texture: {} },
			{ binding: 1, visibility: GPUShaderStage.FRAGMENT, sampler: {} }
		]
	} );
	const pipelineLayout = created.createPipelineLayout( { bindGroupLayouts: [ layout ] } );
	let pipeline: GPURenderPipeline | null = null;
	const ready = module.getCompilationInfo().then( info => {
		const errors = info.messages.filter( m => m.type === "error" );
		if ( errors.length ) throw Error( errors.map( m => m.message ).join( "\n" ) );
		pipeline = created.createRenderPipeline( {
			label: "presentation-finish",
			layout: pipelineLayout,
			vertex: { module, entryPoint: "vs" },
			fragment: { module, entryPoint: "fs", targets: [ { format } ] },
			primitive: { topology: "triangle-list" }
		} );
	} );
	// The offscreen frame texture is stable for one surface size; rebuilding
	// the binding only when that texture changes keeps present() allocation-free.
	let bound: GPUTexture | null = null,
		binding: GPUBindGroup | null = null,
		disposed = false;
	return {
		ready,
		present( source, target ) {
			if ( disposed || !pipeline ) throw Error( "Presentation finish is not ready" );
			if ( source !== bound ) {
				bound = source;
				const view = source.createView();
				binding = created.createBindGroup( {
					layout,
					entries: [ { binding: 0, resource: view }, { binding: 1, resource: sampler } ]
				} );
			}
			const encoder = created.createCommandEncoder( { label: "presentation-finish" } );
			const pass = encoder.beginRenderPass( {
				label: "presentation-finish",
				colorAttachments: [ { view: target.createView(), loadOp: "clear", storeOp: "store" } ]
			} );
			pass.setPipeline( pipeline );
			pass.setBindGroup( 0, binding! );
			pass.draw( 3 );
			pass.end();
			created.queue.submit( [ encoder.finish() ] );
		},
		dispose() {
			disposed = true;
			bound = null;
			binding = null;
		}
	};
}
