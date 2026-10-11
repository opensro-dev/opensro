/*
===========================================================================

pipelines.ts - the device's render pipelines and shaders

Owns the WGSL for world, character and effect geometry, sky, images and
mip generation, and the samplers they bind.

===========================================================================
*/
import { NATIVE_CHARACTER_LIGHTING } from "@/engine/foundation/rendering/video-options";
import {
	D3DBLEND_BLENDFACTOR,
	D3DBLEND_BOTHINVSRCALPHA,
	D3DBLEND_BOTHSRCALPHA,
	D3DBLEND_DESTALPHA,
	D3DBLEND_DESTCOLOR,
	D3DBLEND_INVBLENDFACTOR,
	D3DBLEND_INVDESTALPHA,
	D3DBLEND_INVDESTCOLOR,
	D3DBLEND_INVSRCALPHA,
	D3DBLEND_INVSRCCOLOR,
	D3DBLEND_ONE,
	D3DBLEND_SRCALPHA,
	D3DBLEND_SRCALPHASAT,
	D3DBLEND_SRCCOLOR,
	D3DBLEND_ZERO,
	type BlendPair,
	validBlend
} from "@/engine/foundation/rendering/blend-state";

// The pair a material that blends without naming one draws with.
export const DEFAULT_BLEND: BlendPair = Object.freeze( {
	source: D3DBLEND_SRCALPHA,
	destination: D3DBLEND_INVSRCALPHA
} );

// Experimental video stages (Experimental > Image / World). Each is off by
// default, which is the native frame; env.stages carries the switches
// (x height fog; y sun direction; z terrain relief; w textured horizon).
// Water reflection and equipment shine are native options (Video slots 4
// and 6), not experimental stages.
//
// Height fog - a deliberate deviation from the retail D3DFOG_LINEAR the
// native client authored (sub_4dc920 start/end): the same start/end
// uniforms drive an exp2 falloff with a height term and a horizon tint,
// which keeps the mid-range clear, softens the horizon and lets peaks
// rise out of the haze. Off, the retail linear ramp and colours stand.
const FOG_EXP2_REACH = 2.5; // exp2 factor reaches 1 - 1/255 at the fog end
const FOG_HEIGHT_FALLOFF = 0.004; // fog density e-folds 250 m above the eye
const FOG_SKY_TINT = 0.3; // global fog colour blended toward the horizon colour

// Port-only, not native: Sun direction replaces the pinned diagonal light
// (0.707, 0.707, 0) for every surface and every hour; only its colour
// animates. The deviation rides the sky's sun arc (env.sunDirection,
// world-environment.ts packs it) so dawn and dusk rake across geometry.
//
// Port-only, not native: Terrain relief shades retail NOLIGHT ground.
// The deviation shades terrain slopes against the light
// direction using the heightfield normals the worker now computes,
// normalized against the flat-ground term so level ground keeps its
// retail brightness and only slopes move. The strength is the whole
// tuning surface.
const TERRAIN_RELIEF = 0.45;
const TERRAIN_RELIEF_MIN_LIGHT = 0.0001;

// Port-only, not native: Textured horizon changes the flat fog applied to
// terrain past the detail band (8ABFD0): 2005 minification aliased there.
// Authored mip levels remain unchanged. Single-level terrain textures can
// alias at grazing angles when the textured band is exposed. Off, the
// native flat fog return stands.

// Port-only, not native: the second experimental stages vec4 (byte 368 of
// the environment block; env.stages2 in WGSL) switches the three lighting
// stages of the 2026-10-08 wave: x the HDR intermediate with its tone map,
// y the sun shadow cascade, z per-pixel character and object lighting.
//
// HDR - the scene renders into an rgba16float intermediate and the
// presentation applies a filmic tone map. The 2005 frame is authored
// display-referred LDR, so the only values above 1.0 are the stages that
// lift them (the sun disc gain below); the tone map is the fitted ACES
// curve of hdr.ts, shared by the bloom owner's float composite.
const SUN_HDR_GAIN = 4.0; // sun disc radiance multiplier while the HDR stage is on

// Sun shadow - one orthographic cascade covering SHADOW_EXTENT world units
// around the eye (sun-shadow-math.ts builds the matrix), 2048 taps with a
// 3x3 hardware PCF, fading to unshadowed at the cascade border so the
// boundary never shows a hard step.
const SUN_SHADOW_STRENGTH = 1.0; // fraction of the diffuse term a full shadow removes
const SUN_SHADOW_TEXEL = 1.0 / 2048.0; // one cascade tap in shadow-map UV space
const SUN_SHADOW_FADE = 0.45; // border fraction where the cascade fade begins

// Anisotropic filtering - the experimental sampler level; retail is 1.
const ANISOTROPY = 16;

/*
================
blendFactor

One D3DBLEND factor as WebGPU's. The back buffer is X8R8G8B8, so
DESTALPHA reads 1 and INVDESTALPHA 0; BLENDFACTOR reads the pass's blend
constant (D3DRS_BLENDFACTOR, which the frame owner holds).
================
*/
function blendFactor( factor: number ): GPUBlendFactor {
	switch ( factor ) {
		case D3DBLEND_ZERO:
		case D3DBLEND_INVDESTALPHA:
			return "zero";
		case D3DBLEND_ONE:
		case D3DBLEND_DESTALPHA:
			return "one";
		case D3DBLEND_SRCCOLOR:
			return "src";
		case D3DBLEND_INVSRCCOLOR:
			return "one-minus-src";
		case D3DBLEND_SRCALPHA:
			return "src-alpha";
		case D3DBLEND_INVSRCALPHA:
			return "one-minus-src-alpha";
		case D3DBLEND_DESTCOLOR:
			return "dst";
		case D3DBLEND_INVDESTCOLOR:
			return "one-minus-dst";
		case D3DBLEND_SRCALPHASAT:
			return "src-alpha-saturated";
		case D3DBLEND_BLENDFACTOR:
			return "constant";
		case D3DBLEND_INVBLENDFACTOR:
			return "one-minus-constant";
		default:
			throw Error( "Undefined D3D blend factor" );
	}
}

/*
================
blendState

A D3D9 SRCBLEND/DESTBLEND pair as the WebGPU blend the fixed-function
pipeline applies. Without D3DRS_SEPARATEALPHABLENDENABLE alpha blends with
the same factors; BOTHSRCALPHA and BOTHINVSRCALPHA, as the source, set
both factors.
================
*/
export function blendState( pair: BlendPair ): GPUBlendState {
	if ( !validBlend( pair ) ) throw Error( "Undefined D3D blend pair" );
	let source = pair.source, destination = pair.destination;
	if ( source === D3DBLEND_BOTHSRCALPHA ) {
		source = D3DBLEND_SRCALPHA;
		destination = D3DBLEND_INVSRCALPHA;
	} else if ( source === D3DBLEND_BOTHINVSRCALPHA ) {
		source = D3DBLEND_INVSRCALPHA;
		destination = D3DBLEND_SRCALPHA;
	}
	const component: GPUBlendComponent = {
		srcFactor: blendFactor( source ),
		dstFactor: blendFactor( destination ),
		operation: "add"
	};
	return { color: component, alpha: component };
}

/*
================
GeometryPipelineState

What a geometry draw's pipeline varies by: its D3D blend pair (null draws
opaque), back-face culling, depth writes and the depth test.
================
*/
export interface GeometryPipelineState {
	readonly blend: BlendPair | null;
	readonly cull: boolean;
	readonly depthWrite: boolean;
	readonly depthCompare: "less-equal" | "always";
}

/*
================
geometryPipelineKey
================
*/
export function geometryPipelineKey( state: GeometryPipelineState ): string {
	const blend = state.blend ? `${state.blend.source}/${state.blend.destination}` : "opaque";
	return `${blend}|${state.cull ? "cull" : "none"}|${state.depthWrite ? "write" : "keep"}|${state.depthCompare}`;
}

// The states every scene draws with, compiled before the device runs:
// opaque, alpha blended (with and without depth writes), additive, the
// lightmap multiply, and the no-test passes (sky, deferred particles,
// ground decals), each culled and not.
const COMMON_GEOMETRY_STATES: readonly GeometryPipelineState[] = [ false, true ].flatMap( cull => [
	{ blend: null, cull, depthWrite: true, depthCompare: "less-equal" as const },
	{ blend: DEFAULT_BLEND, cull, depthWrite: false, depthCompare: "less-equal" as const },
	{ blend: DEFAULT_BLEND, cull, depthWrite: true, depthCompare: "less-equal" as const },
	{
		blend: { source: D3DBLEND_SRCALPHA, destination: D3DBLEND_ONE },
		cull,
		depthWrite: false,
		depthCompare: "less-equal" as const
	},
	{
		blend: { source: D3DBLEND_ZERO, destination: D3DBLEND_SRCCOLOR },
		cull,
		depthWrite: false,
		depthCompare: "less-equal" as const
	},
	{ blend: DEFAULT_BLEND, cull, depthWrite: false, depthCompare: "always" as const },
	{
		blend: { source: D3DBLEND_SRCALPHA, destination: D3DBLEND_ONE },
		cull,
		depthWrite: false,
		depthCompare: "always" as const
	}
] );
/*
================
createPipelines

Compiles every render pipeline and sampler once per device: the geometry
pipelines (one per material policy and blend), sky, image and mip
generation. The geometry shader mirrors the native fixed-function paths it
names (stage-0 ops in texture-stage.ts, lighting, fog, alpha test).
================
*/
export function createPipelines( created: GPUDevice, format: GPUTextureFormat ) {
	let mipPipeline: GPURenderPipeline | null = null;
	let pipeline: GPURenderPipeline | null = null, sampler: GPUSampler | null = null;
	const shader = created.createShaderModule( {
		code: `
@group(0) @binding(0) var imageSampler:sampler;
@group(0) @binding(1) var imageTexture:texture_2d<f32>;
struct Out {@builtin(position) position:vec4f,@location(0) uv:vec2f}
@vertex fn vs(@builtin(vertex_index) i:u32)->Out {
 let positions=array<vec2f,6>(vec2f(-1,1),vec2f(-1,-1),vec2f(1,-1),vec2f(-1,1),vec2f(1,-1),vec2f(1,1));
 var out:Out;let p=positions[i];out.position=vec4f(p,0,1);out.uv=vec2f((p.x+1)*0.5,(1-p.y)*0.5);return out;
}
@fragment fn fs(input:Out)->@location(0) vec4f {return textureSample(imageTexture,imageSampler,input.uv);}`
	} );
	sampler = created.createSampler( { minFilter: "nearest", magFilter: "nearest" } );
	const prepared = created.createRenderPipelineAsync( {
		layout: "auto",
		vertex: { module: shader, entryPoint: "vs" },
		fragment: { module: shader, entryPoint: "fs", targets: [ { format: format } ] },
		primitive: { topology: "triangle-list" },
		depthStencil: { format: "depth24plus", depthWriteEnabled: false, depthCompare: "always" }
	} );
	const preparedMips = created.createRenderPipelineAsync( {
		layout: "auto",
		vertex: { module: shader, entryPoint: "vs" },
		fragment: { module: shader, entryPoint: "fs", targets: [ { format: "rgba8unorm" } ] },
		primitive: { topology: "triangle-list" }
	} );
	// The HDR stage renders the scene into an rgba16float intermediate, so
	// the scene-drawing pipelines need float-target siblings. Like the float
	// bloom chain, they compile lazily on first enabled use: a native-only
	// device never compiles them. The float target format is the one owner.
	const SCENE_FLOAT_FORMAT: GPUTextureFormat = "rgba16float";
	const environmentStruct =
		`struct Environment {zenith:vec4f,horizon:vec4f,diffuse:vec4f,ambient:vec4f,forward:vec4f,right:vec4f,up:vec4f,fog:vec4f,settings:vec4f,water:vec4f,shadow:vec4f,scatter:vec4f,skyTime:vec4f,sun:vec4f,lunar:vec4f,stars:array<vec4f,3>,terrainFog:vec4f,terrainBand:vec4f,reflection:vec4f,sunDirection:vec4f,stages:vec4f,stages2:vec4f,shadowMatrix:mat4x4f}`;
	const skyShader = created.createShaderModule( {
		code: environmentStruct + `
@group(0) @binding(0) var<uniform> env:Environment;
struct SkyOut {@builtin(position) position:vec4f,@location(0) ray:vec3f}
@vertex fn vs(@builtin(vertex_index) i:u32)->SkyOut {
 let p=array<vec2f,6>(vec2f(-1,1),vec2f(-1,-1),vec2f(1,-1),vec2f(-1,1),vec2f(1,-1),vec2f(1,1))[i];
 var o:SkyOut;o.position=vec4f(p,1,1);o.ray=env.forward.xyz+p.x*env.right.xyz+p.y*env.up.xyz;return o;
}
@fragment fn fs(o:SkyOut)->@location(0) vec4f {return vec4f(mix(env.horizon.rgb,env.zenith.rgb,clamp(normalize(o.ray).y*env.zenith.w,0,1)),1);}`
	} );
	const preparedSky = created.createRenderPipelineAsync( {
		layout: "auto",
		vertex: { module: skyShader, entryPoint: "vs" },
		fragment: { module: skyShader, entryPoint: "fs", targets: [ { format } ] },
		primitive: { topology: "triangle-list" },
		depthStencil: { format: "depth24plus", depthWriteEnabled: false, depthCompare: "always" }
	} );
	let skyPipeline: GPURenderPipeline | null = null;
	// Lazy HDR siblings: like the float bloom chain, they compile only on
	// first enabled use, so a native-only device never pays for them.
	let skyFloatPipeline: GPURenderPipeline | null = null, imageFloatPipeline: GPURenderPipeline | null = null;
	const geometryShader = created.createShaderModule( {
		code: environmentStruct + `
const nativeCharacterLighting:bool=${NATIVE_CHARACTER_LIGHTING};
@group(0) @binding(0) var<uniform> transform:mat4x4f;
struct Instance {matrix:mat4x4f,opacity:vec4f,color:vec4f,window:vec4f,pointPosition:vec4f,pointAmbient:vec4f,pointDiffuse:vec4f}
@group(0) @binding(1) var<storage,read> instances:array<Instance>;
struct Material {color:vec4f,options:vec4f,skin:vec4f,window:vec4f,lighting:vec4f,policy:vec4f,localFog:vec4f,localFogSettings:vec4f,ambient:vec4f,uvU:vec4f,uvV:vec4f,reflection:vec4f,equipmentColor:vec4f,equipmentUV:vec4f,stage:vec4f,stagePolicy:vec4f,textureFactor:vec4f}
@group(0) @binding(2) var<uniform> material:Material;
// texture-stage.ts: D3D9 stage 0, every op it defines. CURRENT is the
// diffuse colour at stage 0; TEMP starts at zero; SPECULAR is the vertex
// specular, which the port's lighting does not produce (zero).
struct StageInputs {texture:vec4f,diffuse:vec4f,specular:vec4f,factor:vec4f}
fn stageArgument(arg:u32,i:StageInputs)->vec4f {
 let selector=arg&15u;var value=i.diffuse;
 if(selector==2u){value=i.texture;}else if(selector==3u){value=i.factor;}else if(selector==4u){value=i.specular;}else if(selector==5u){value=vec4f(0);}
 if((arg&32u)!=0u){value=vec4f(value.a);}
 if((arg&16u)!=0u){value=vec4f(1)-value;}
 return value;
}
// validTextureStage admits only the ops listed here.
fn stageEvaluate(op:u32,a:vec4f,b:vec4f,a0:vec4f,i:StageInputs)->vec4f {
 switch(op){
  case 2u:{return a;} case 3u:{return b;} case 4u:{return a*b;} case 5u:{return a*b*2.0;} case 6u:{return a*b*4.0;}
  case 7u:{return a+b;} case 8u:{return a+b-0.5;} case 9u:{return (a+b-0.5)*2.0;} case 10u:{return a-b;}
  case 11u:{return a+b-a*b;} case 12u,16u:{return a*i.diffuse.a+b*(1.0-i.diffuse.a);}
  case 13u:{return a*i.texture.a+b*(1.0-i.texture.a);} case 14u:{return a*i.factor.a+b*(1.0-i.factor.a);}
  case 15u:{return a+b*(1.0-i.texture.a);}
  case 18u:{return a+a.a*b;} case 19u:{return a*b+a.a;} case 20u:{return (1.0-a.a)*b+a;} case 21u:{return (1.0-a)*b+a.a;}
  case 24u:{return vec4f(4.0*dot(a.rgb-0.5,b.rgb-0.5));}
  case 25u:{return a0+a*b;} case 26u:{return a0*a+(1.0-a0)*b;}
  default:{return a*b;}
 }
}
fn stageColor(stage:vec4f,i:StageInputs)->vec4f {
 let colorOp=u32(stage.x);let colorArgs=u32(stage.y);let alphaOp=u32(stage.z);let alphaArgs=u32(stage.w);
 if(colorOp==1u){return i.diffuse;}
 let rgb=clamp(stageEvaluate(colorOp,stageArgument(colorArgs/64u,i),stageArgument(colorArgs%64u,i),i.diffuse,i),vec4f(0),vec4f(1)).rgb;
 if(colorOp==24u){return vec4f(rgb,rgb.r);}
 if(alphaOp==1u){return vec4f(rgb,i.diffuse.a);}
 let alpha=clamp(stageEvaluate(alphaOp,stageArgument(alphaArgs/64u,i),stageArgument(alphaArgs%64u,i),i.diffuse,i),vec4f(0),vec4f(1)).a;
 return vec4f(rgb,alpha);
}
@group(0) @binding(3) var textureSampler:sampler;
@group(0) @binding(4) var albedo:texture_2d_array<f32>;
@group(0) @binding(5) var<uniform> env:Environment;
// The sun shadow cascade (experimental). The comparison sampler resolves
// hardware PCF taps; the dummy 1x1 depth view every draw binds while the
// stage is off compares against cleared depth 1.0, so the gated branch
// below never runs and the native frame is untouched.
@group(0) @binding(13) var sunShadowMap:texture_depth_2d;
@group(0) @binding(14) var sunShadowSampler:sampler_comparison;
// The retail light is the pinned 45-degree diagonal; the sun-direction
// stage replaces it with the arc direction env carries (see the stage
// comment above the constants).
fn lightDirection()->vec3f{
 // world-environment.ts packs a unit direction that stays continuous through
 // the horizon crossing (it blends toward the zenith there).
 return select(vec3f(0.70710678,0.70710678,0),env.sunDirection.xyz,env.stages.y>0.5);
}
// Port-only, not native: the sun shadow factor of one world position. The
// cascade matrix (sun-shadow-math.ts, texel-snapped around the eye) maps
// region-relative world coordinates to [0,1] cascade UVZ; outside the
// cascade the surface stays lit, inside it a 3x3 kernel of hardware
// comparison taps resolves the penumbra, fading at the border.
fn sunShadowFactor(world:vec3f)->f32{
 // The cascade matrix (sun-shadow-math.ts) is a WebGPU orthographic
 // projection: NDC x/y the caster pass rasterizes, [0,1] depth both sides
 // compare. Receivers sample at the centred UV.
 let clip=env.shadowMatrix*vec4f(world,1.0);
 // WebGPU rasterization maps positive NDC Y to the top of the texture.
 let uv=vec2f(clip.x*0.5+0.5,0.5-clip.y*0.5);
 if(uv.x<0.0||uv.x>1.0||uv.y<0.0||uv.y>1.0||clip.z>1.0||clip.z<0.0){return 1.0;}
 var sum=0.0;
 for(var y:i32=-1;y<=1;y++){
  for(var x:i32=-1;x<=1;x++){
   sum+=textureSampleCompareLevel(sunShadowMap,sunShadowSampler,uv+vec2f(f32(x),f32(y))*${SUN_SHADOW_TEXEL},clip.z);
  }
 }
 let border=1.0-smoothstep(${SUN_SHADOW_FADE},0.5,max(abs(uv.x-0.5),abs(uv.y-0.5)));
 return mix(1.0,sum/9.0,border);
}
struct SkinVertex {joints:vec4u,weights:vec4f}
@group(0) @binding(6) var<storage,read> skinVertices:array<SkinVertex>;
@group(0) @binding(7) var<storage,read> bones:array<mat4x4f>;
@group(0) @binding(8) var sphereMap:texture_2d_array<f32>;
@group(0) @binding(9) var waterMirror:texture_2d<f32>;
struct WaterPass {matrix:mat4x4f,plane:vec4f,bump:vec4f,projection:vec4f}
@group(0) @binding(10) var<uniform> waterPass:WaterPass;
// 8BA130: stage 0 bump clamps; stage 1 reflection wraps. Both filter linearly.
@group(0) @binding(11) var waterBumpSampler:sampler;
@group(0) @binding(12) var waterMirrorSampler:sampler;
struct Out {@location(11) worldY:f32,@location(12) mirrorPosition:vec4f,@builtin(position) position:vec4f,@location(0) uv:vec2f,@location(1) normal:vec3f,@location(2) color:vec4f,@location(3) maskUV:vec2f,@location(4) viewZ:f32,@location(5) objectLighting:vec3f,@location(6) @interpolate(flat) opacity:f32,@location(7) worldXZ:vec2f,@location(8) @interpolate(flat) materialTint:vec3f,@location(9) sphereUV:vec2f,@location(10) equipmentUV:vec2f}
@vertex fn vs(@location(0) position:vec3f,@location(1) normal:vec3f,@location(2) uv:vec2f,@location(3) color:vec4f,@location(4) maskUV:vec2f,@builtin(instance_index) i:u32,@builtin(vertex_index) vertex:u32)->Out {
 var o:Out;let instance=instances[i].matrix;o.opacity=instances[i].opacity.x;var p=vec4f(position,1);var n=vec4f(normal,0);
 if(material.skin.x!=0){let v=skinVertices[vertex];let base=select(select(i*u32(abs(material.skin.x)),0u,material.skin.x<0),u32(instances[i].opacity.y),instances[i].opacity.z>0);let skin=bones[base+v.joints.x]*v.weights.x+bones[base+v.joints.y]*v.weights.y+bones[base+v.joints.z]*v.weights.z+bones[base+v.joints.w]*v.weights.w;p=skin*p;n=skin*n;}
 o.worldY=(instance*p).y;
 o.worldXZ=(instance*p).xz;o.position=transform*instance*p;o.normal=(instance*n).xyz;let movingUV=vec2f(dot(vec3f(uv,1),material.uvU.xyz),dot(vec3f(uv,1),material.uvV.xyz));o.uv=(movingUV*material.window.xy+material.window.zw)*instances[i].window.xy+instances[i].window.zw;o.materialTint=select(vec3f(1),instances[i].color.rgb,material.policy.w>0.5);o.color=color*select(instances[i].color,vec4f(1,1,1,instances[i].color.a),material.policy.w>0.5);o.maskUV=maskUV;o.viewZ=o.position.w;
 // Undo only projection scaling to recover the native camera-space position.
 let projectionScale=vec2f(length(vec3f(transform[0].x,transform[1].x,transform[2].x)),length(vec3f(transform[0].y,transform[1].y,transform[2].y)));
 o.mirrorPosition=vec4f(o.position.xy/projectionScale,0,o.position.w);
 // Native vs_1_1 oD0: light and saturate each vertex before interpolation.
 var lightingNormal=n.xyz;
 // Data.pk2 vss2.c normalizes the blended normal; vss0.c does not.
 if(material.skin.x!=0){lightingNormal=normalize(lightingNormal);}
 // A61887 + A5F103: transpose(world*view*diag(.8,.8,1));
 // native shader SPEC adds .5 after transforming, without renormalizing.
 o.equipmentUV=uv+material.equipmentUV.xy;
 o.sphereUV=vec2f(0);
 if(material.reflection.x>0.5&&env.reflection.w>0.5){
  let reflectionNormal=(instance*vec4f(lightingNormal,0)).xyz;
  let cameraRight=normalize(vec3f(transform[0].x,transform[1].x,transform[2].x));
  let cameraUp=normalize(vec3f(transform[0].y,transform[1].y,transform[2].y));
  o.sphereUV=vec2f(dot(reflectionNormal,cameraRight),dot(reflectionNormal,cameraUp))*0.8+vec2f(0.5);
 }
 // A5D450 uploads inverse(world)*light to c9. Its dual here is
 // inverse-transpose(world)*normal; do not normalize again after scale.
 let a=instance[0].xyz;let b=instance[1].xyz;let c=instance[2].xyz;
 let cofactors=mat3x3f(cross(b,c),cross(c,a),cross(a,b));let determinant=dot(a,cross(b,c));
 lightingNormal=cofactors*lightingNormal/select(1.0,determinant,determinant!=0.0);
 // Port-only, not native: interpolate the inverse-transpose normal for
 // per-pixel object lighting. Multiplying by world skews scaled objects'
 // normals; leave the original varying untouched when this stage is off.
 if(env.stages2.z>0.5&&material.lighting.y>0.5){o.normal=lightingNormal;}
 // A91970 replaces BMT diffuse and actor ambient during SCT_MAT.
 let diffuseFactor=select(material.color.rgb,o.materialTint,material.policy.w>0.5);
 let ambientFactor=select(material.ambient.rgb,o.materialTint,material.policy.w>0.5);
	var illumination=diffuseFactor*env.diffuse.rgb*max(0.0,dot(lightingNormal,lightDirection()))+env.ambient.rgb*ambientFactor*material.lighting.x;
 let light=instances[i];
 if(nativeCharacterLighting&&(any(light.pointAmbient.rgb!=vec3f(0))||any(light.pointDiffuse.rgb!=vec3f(0)))){
  let localLight=transpose(cofactors)*(light.pointPosition.xyz-instance[3].xyz)/select(1.0,determinant,determinant!=0.0);
  let delta=localLight-p.xyz;let distance=length(delta);let normalObject=select(n.xyz,normalize(n.xyz),material.skin.x!=0);
  let lambert=select(0.0,max(0.0,dot(normalObject,delta/max(distance,0.000001)))/max(light.pointPosition.w*distance,0.000001),distance>0.0);
  illumination+=light.pointAmbient.rgb+diffuseFactor*light.pointDiffuse.rgb*lambert;
 }
 o.objectLighting=clamp(illumination,vec3f(0),vec3f(1));
 if(waterPass.plane.w>0.5){o.position=waterPass.matrix*instance*p;o.viewZ=o.position.w;}
 if(material.skin.w>0){
  let k=material.skin.w;var skyPosition=position;let angle=(env.skyTime.x-0.25)*6.28318530718;let sunPosition=vec3f(cos(angle),sin(angle),0)*20000.0;
  if(k==3||k==4){var a=angle;if(k==4){let w=fract(env.skyTime.x+0.25);a=(0.25+(w-0.25)*1.2)*6.28318530718;}
   let center=vec3f(cos(a),sin(a),0)*20000.0;skyPosition=center+vec3f(-position.y*sin(a),position.y*cos(a),position.z)*select(666.666687,1000.0,k==4);
  }
  if(waterPass.plane.w>0.5&&waterPass.plane.y>0){skyPosition.y=-skyPosition.y;}
  let z=dot(skyPosition,env.forward.xyz);o.position=vec4f(dot(skyPosition,env.right.xyz)/dot(env.right.xyz,env.right.xyz),dot(skyPosition,env.up.xyz)/dot(env.up.xyz,env.up.xyz),z,z);o.viewZ=z;
  if(k==1){let base=mix(env.horizon.rgb,env.zenith.rgb,clamp(position.y/5000.0*env.sun.w,0,1));o.color=vec4f(mix(env.scatter.rgb,base,min(1.0,distance(position,sunPosition)/env.scatter.w)),1);}
  if(k==2){let batch=u32(maskUV.x);let alpha=env.stars[batch/4u][batch%4u];o.color.a*=f32(u32(max(0.0,alpha*env.skyTime.w))&255u)/255.0;o.position=vec4f(o.position.xy+uv*maskUV.y*env.lunar.yz*z,o.position.zw);}
  if(k==5){o.uv=uv+vec2f(env.skyTime.y);}
  if(waterPass.plane.w>0.5&&waterPass.plane.y>0){o.position.y=-o.position.y;}
 }
 return o;
}
fn shadeGeometry(input:Out)->vec4f {
 if(waterPass.plane.w>0.5&&material.skin.w==0&&(input.worldY-waterPass.plane.x)*waterPass.plane.y<0){discard;}
 var reflected=vec3f(0);var equipment=vec3f(0);
 if(material.equipmentColor.w>0.0){equipment=textureSample(sphereMap,textureSampler,input.equipmentUV,0).rgb;}
 // Keep implicit-derivative sampling behind uniform gates, before varying exits.
 if(material.reflection.x>0.5&&env.reflection.w>0.5){reflected=textureSample(sphereMap,textureSampler,input.sphereUV,0).rgb;}
 var waterColor=vec3f(0);
 if(material.skin.z>0.5&&waterPass.plane.z>0.5&&waterPass.plane.w<0.5){let projected=input.mirrorPosition.xy/input.mirrorPosition.w;var offset=vec2f(0);
 // 8BA130 cell corners use bump UV (0,1)..(1,0); ordinary water tiles four times.
 if(material.reflection.w>0.5){let dudv=(textureSampleLevel(sphereMap,waterBumpSampler,input.uv*vec2f(.25,-.25)+vec2f(0,1),0,0).rg*255.0-128.0)/127.0;offset=vec2f(dot(waterPass.bump.xy,dudv),dot(waterPass.bump.zw,dudv));}
 waterColor=textureSampleLevel(waterMirror,waterMirrorSampler,projected*waterPass.projection.xy+waterPass.projection.zw+offset,0).rgb;}
 let layer=select(u32(env.settings.z),u32(env.lunar.x),material.skin.w==4.0);let tex=textureSample(albedo,textureSampler,input.uv,i32(layer%textureNumLayers(albedo)));
 if(material.skin.w>0){let k=material.skin.w;
  if(k==1||k==2){return input.color;}
  if(k==3){
   // HDR (experimental): the disc leaves the clamped authored range so the
   // tone map and the bloom see real energy; off, the retail clamp stands.
   let disc=select(clamp(env.sun.rgb,vec3f(0),vec3f(1)),env.sun.rgb*${SUN_HDR_GAIN},env.stages2.x>0.5);
   return vec4f(disc,tex.a*select(0.0,1.0,env.skyTime.x>=0.25&&env.skyTime.x<=0.875));
  }
  if(k==4){return vec4f(tex.rgb,tex.a*select(0.0,1.0,(env.skyTime.x>0.75||env.skyTime.x<0.5)&&env.lunar.x<29.0));}
  if(k==5){return vec4f(mix(clamp(env.scatter.rgb,vec3f(0),vec3f(1))*tex.rgb,env.fog.rgb,clamp((input.viewZ-90000.0)/110000.0,0,1)),tex.a*clamp(env.skyTime.z,0,1));}
  return vec4f(env.fog.rgb,input.color.a);
 }
var light=vec4f(1);if(material.skin.y>0.5){light=textureSampleBias(albedo,textureSampler,input.uv,0,-0.5);}
 // Retail 8ABFD0: cells outside the detailed band use untextured linear fog.
 // The textured-horizon stage keeps the textured fog blend instead;
 // authored single-level textures can alias at grazing angles.
 let cellDelta=floor(input.worldXZ/320.0)-env.terrainBand.xy;
 let distant=env.terrainBand.w>0.5&&dot(cellDelta,cellDelta)>env.terrainBand.z&&env.stages.w<0.5;
 if(distant&&material.skin.y>0.5){discard;}
 if(distant&&material.options.z>0.5){return vec4f(env.fog.rgb,1);}
let mask=mix(mix(input.color.x,input.color.y,input.maskUV.x),mix(input.color.z,input.color.w,input.maskUV.x),input.maskUV.y);var color=vec4f(tex.rgb,select(tex.a,1.0,material.policy.z>0.5))*material.color*select(input.color,vec4f(1,1,1,mask),material.options.z>0.5);
		let illumination=clamp(select(material.color.rgb,input.materialTint,material.policy.w>0.5)*env.diffuse.rgb*max(0.0,dot(normalize(input.normal),lightDirection()))+env.ambient.rgb*select(material.ambient.rgb,input.materialTint,material.policy.w>0.5)*material.lighting.x,vec3f(0),vec3f(1));
 var surfaceLight=select(illumination,input.objectLighting,material.lighting.y>0.5);
 // Per-pixel character lighting (experimental): the retail vs_1_1 path
 // saturates each vertex's lighting before interpolation (o.objectLighting,
 // A5D450's diagonal in object space); the stage re-evaluates the same
 // terms per pixel from the interpolated, skinned world normal. The vertex
 // point-light path (NATIVE_CHARACTER_LIGHTING) stays per-vertex: it is a
 // compatibility mode of its own and is off.
 if(env.stages2.z>0.5&&material.lighting.y>0.5){
  surfaceLight=illumination;
 }
 // Sun shadows (experimental): the cascade shades the diffuse term and
 // leaves the ambient standing, for the world path and the object path
 // alike. reflection.z admits only world-space draws: portrait and doll
 // positions cannot sample this scene's cascade. Water and sky never receive.
 var sunShade=1.0;
 if(env.stages2.y>0.5&&material.reflection.z>0.5&&material.skin.z==0.0&&material.skin.w==0.0){
  sunShade=mix(1.0,sunShadowFactor(vec3f(input.worldXZ.x,input.worldY,input.worldXZ.y)),${SUN_SHADOW_STRENGTH});
  let ambientTerm=env.ambient.rgb*select(material.ambient.rgb,input.materialTint,material.policy.w>0.5)*material.lighting.x;
  surfaceLight=ambientTerm+(surfaceLight-ambientTerm)*sunShade;
 }
 // B153A0 (effects) and sub_aed240 (BSR material modifiers) set stage 0 from
 // the resource. Its DIFFUSE is the vertex colour as lit, or unlit the
 // material colour (NOLIGHT object lighting writes oD0 = 1, no tint).
 if(material.stage.x>0.0){
  let unlitDiffuse=select(material.color*select(input.color,vec4f(1,1,1,mask),material.options.z>0.5),input.color,material.lighting.y>0.5);
  let fixedDiffuse=select(vec4f(input.color.rgb*surfaceLight,unlitDiffuse.a),unlitDiffuse,material.options.y>0.5);
  // A BSR vertex shader's oD0: N.L*c11+c10 with both w at 1, or 1 for NOLIGHT.
  let shaderDiffuse=select(vec4f(surfaceLight,1),vec4f(1),material.options.y>0.5);
  let diffuse=select(fixedDiffuse,shaderDiffuse,material.stagePolicy.x>0.5);
  color=stageColor(material.stage,StageInputs(tex,diffuse,vec4f(0),material.textureFactor));
 }
 let fading=material.lighting.z>0.5&&input.opacity<1.0;
 // An effect fades its own stage alpha (diffuse animation included).
 let fadeAlpha=select(select(tex.a,1.0,material.lighting.w>0.5),color.a,material.stage.x>0.0)*input.opacity;
 let cutoff=select(material.options.x,floor(material.options.x*255.0*round(input.opacity*255.0)/256.0)/255.0,fading);
 // D3D9 HAL alpha-test conversion: truncate to 12 fractional bits, then
 // nearest UNORM8 with half-way values down. Keep blending alpha unmodified.
 let alphaFixed=floor(clamp(select(color.a,fadeAlpha,fading),0.0,1.0)*4096.0);
 let alphaByte=ceil(alphaFixed*(255.0/4096.0)-0.5);
 let reference=round(cutoff*255.0);var accepted=true;
 switch(u32(material.reflection.y)){
  case 1u:{accepted=false;} case 2u:{accepted=alphaByte<reference;}
  case 3u:{accepted=alphaByte==reference;} case 4u:{accepted=alphaByte<=reference;}
  case 5u:{accepted=alphaByte>reference;} case 6u:{accepted=alphaByte!=reference;}
  case 8u:{} default:{accepted=alphaByte>=reference;}
 }
 if(material.equipmentColor.w>0.0){accepted=material.equipmentUV.z<0.5||ceil(floor(clamp(tex.a,0.0,1.0)*4096.0)*(255.0/4096.0)-0.5)>=1.0;}
 if(!accepted){discard;}
 // Native NOLIGHT writes oD0=1, bypassing the material diffuse tint too.
 let unlitColor=select(color.rgb,tex.rgb*input.color.rgb,material.lighting.y>0.5);
 // A native stage is the whole colour: its op already carries any 2X/4X.
 var lit=select(clamp(select(tex.rgb*input.color.rgb*surfaceLight,unlitColor,material.options.y>0.5)*material.options.w,vec3f(0),vec3f(1)),color.rgb,material.stage.x>0.0);
 // Terrain relief (experimental): retail ground is NOLIGHT - flat albedo the
 // lightmap then multiplies. The stage shades slopes against the light
 // direction through the heightfield normals the worker computes, levelled
 // against the flat-ground term so level terrain keeps its retail brightness.
 if(env.stages.z>0.5&&material.options.z>0.5){
  // The flat-ground term follows the SAME light as the slope term: level
  // ground's response is dot((0,1,0), L), which is 0.707 under the retail
  // diagonal and the sun's height under the arc - level terrain keeps its
  // brightness at every hour.
  let reliefBase=env.ambient.rgb+env.diffuse.rgb*max(0.0,lightDirection().y);
  let reliefSlope=env.ambient.rgb+env.diffuse.rgb*max(0.0,dot(normalize(input.normal),lightDirection()));
  // With no light in a channel there is no slope contrast to apply.
  let reliefRatio=select(vec3f(1),reliefSlope/max(reliefBase,vec3f(${TERRAIN_RELIEF_MIN_LIGHT})),reliefBase>vec3f(${TERRAIN_RELIEF_MIN_LIGHT}));
  lit*=mix(vec3f(1),clamp(reliefRatio,vec3f(0),vec3f(2)),${TERRAIN_RELIEF});
 }
 // AEE6D0: stage0 sphere*TFACTOR; stage1 base+base.a*current;
 // stage2 MODULATE2X with saturated vertex diffuse. Opacity gates RGB only.
 if(material.reflection.x>0.5&&env.reflection.w>0.5&&input.opacity==1.0){
  // 85A9E0 sets temporary renderer light1 (C39094+64 -> A5A7F0).
  // It does not set the model's +1E4 reflection override (A89560).
  // Neither ordinary hit lights nor SCT_MAT replace this factor.
  let factor=env.reflection.rgb;
  let stage0=clamp(reflected*factor,vec3f(0),vec3f(1));
  let stage1=clamp(tex.rgb+tex.a*stage0,vec3f(0),vec3f(1));
  lit=clamp(stage1*select(surfaceLight,vec3f(1),material.options.y>0.5)*input.color.rgb*2.0,vec3f(0),vec3f(1));
 }
 // AEF8E0: option texture * packed TFACTOR (MODULATE/2X), then ADD base.
 if(material.equipmentColor.w>0.0){lit=clamp(tex.rgb+clamp(equipment*material.equipmentColor.rgb*material.equipmentColor.w,vec3f(0),vec3f(1)),vec3f(0),vec3f(1));}
 let animated=material.skin.z>0.5;
 let waterShading=clamp(env.water.rgb,vec3f(0),vec3f(1));
 let fogSource=select(env.fog,material.localFog,material.policy.y>0.5);
 let fogEnd=select(env.settings.x,material.localFogSettings.x,material.policy.y>0.5);
 // Native D3DFOG_LINEAR between the authored start and end.
 let fogApplies=(env.settings.y>0.5||material.policy.y>0.5)&&material.policy.x<0.5;
 var fog=select(0.0,clamp((input.viewZ-fogSource.w)/max(0.001,fogEnd-fogSource.w),0,1),fogApplies);
 var fogColor=fogSource.rgb;var terrainFogColor=env.terrainFog.rgb;
 if(env.stages.x>0.5){
  // Height fog (experimental): exp2 with no fog before the authored start,
  // 1-1/255 at the end, the density e-folding above the eye so peaks clear
  // the haze. The global fog takes the horizon tint; local fog volumes keep
  // their authored colour, and both fog targets tint identically so the
  // distant terrain band's colour match keeps its seam behaviour.
  let density=${FOG_EXP2_REACH}/max(0.001,fogEnd-fogSource.w);
  let height=exp(-max(0.0,input.worldY-env.settings.w)*${FOG_HEIGHT_FALLOFF});
  let d=max(0.0,input.viewZ-fogSource.w)*density*height;
  fog=select(0.0,1.0-exp(-d*d),fogApplies);
  fogColor=select(fogSource.rgb,mix(fogSource.rgb,env.horizon.rgb,${FOG_SKY_TINT}),material.policy.y<0.5);
  terrainFogColor=mix(env.terrainFog.rgb,env.horizon.rgb,${FOG_SKY_TINT});
 }
 if(material.skin.y>0.5){
  // Lightmap terrain receives the same cascade: the baked light sum is the
  // diffuse term here, so the shade multiplies it ahead of the fog blend.
  let litTerrain=clamp(light.rgb+env.shadow.rgb,vec3f(0),vec3f(1))*sunShade;
  return vec4f(mix(litTerrain,terrainFogColor,fog),1);
 }
 if(animated&&waterPass.plane.z>0.5&&waterPass.plane.w<0.5){lit=waterColor;}
 return vec4f(mix(lit*select(vec3f(1),waterShading,animated),select(fogColor,terrainFogColor,material.options.z>0.5),fog),select(select(color.a,clamp(input.color.a,0,1),animated),select(select(1.0,color.a,material.ambient.w>0.5),fadeAlpha,fading),material.lighting.z>0.5));
}
@fragment fn fs(input:Out)->@location(0) vec4f {return shadeGeometry(input);}
// Port-only, not native: an accepted unblended/cutout fragment covers the
// scratch target fully. Alpha tests still discard inside shadeGeometry.
@fragment fn fsPreviewOpaque(input:Out)->@location(0) vec4f {
 let color=shadeGeometry(input);return vec4f(color.rgb,1.0);
}`
	} );
	/*
	================
	geometryDescriptor

	The geometry pipeline for one state: the material's D3D blend pair (or
	none), culling, depth writes and depth test.
	================
	*/
	const geometryDescriptor = (
		state: GeometryPipelineState,
		target: GPUTextureFormat = format,
		previewCoverage = false
	): GPURenderPipelineDescriptor => {
		const blend = state.blend ? blendState( state.blend ) : undefined;
		const overwrite = blend?.color.srcFactor === "one" && blend.color.dstFactor === "zero";
		// Port-only, not native: scratch RGB is already premultiplied by
		// source-over blending. Accumulate coverage instead of squaring alpha.
		// Additive draws add radiance without occluding the background. Other
		// authored blends and every world/native target keep their state.
		if (
			previewCoverage && blend?.color.srcFactor === "src-alpha" &&
			blend.color.dstFactor === "one-minus-src-alpha"
		) {
			blend.alpha = { srcFactor: "one", dstFactor: "one-minus-src-alpha", operation: "add" };
		} else if ( previewCoverage && blend?.color.dstFactor === "one" ) {
			blend.alpha = { srcFactor: "zero", dstFactor: "one", operation: "add" };
		}
		return {
			layout: "auto",
			vertex: {
				module: geometryShader,
				entryPoint: "vs",
				buffers: [ {
					arrayStride: 56,
					attributes: [
						{ shaderLocation: 0, offset: 0, format: "float32x3" },
						{ shaderLocation: 1, offset: 12, format: "float32x3" },
						{ shaderLocation: 2, offset: 24, format: "float32x2" },
						{ shaderLocation: 3, offset: 32, format: "float32x4" },
						{ shaderLocation: 4, offset: 48, format: "float32x2" }
					]
				} ]
			},
			fragment: {
				module: geometryShader,
				entryPoint: previewCoverage && (!blend || overwrite) ? "fsPreviewOpaque" : "fs",
				targets: [ { format: target, ...(blend ? { blend } : {}) } ]
			},
			primitive: {
				topology: "triangle-list",
				cullMode: state.cull ? "back" : "none",
				frontFace: "cw"
			},
			depthStencil: {
				format: "depth24plus",
				depthWriteEnabled: state.depthWrite,
				depthCompare: state.depthCompare
			}
		};
	};
	// Geometry pipelines by state. The states every scene uses are compiled
	// before the device runs; any other is compiled at its first upload.
	const geometryPipelines = new Map<string, GPURenderPipeline>();
	const preparedGeometry = Promise.all(
		COMMON_GEOMETRY_STATES.map( state =>
			created.createRenderPipelineAsync( geometryDescriptor( state ) ).then( pipeline => {
				geometryPipelines.set( geometryPipelineKey( state ), pipeline );
			} )
		)
	);
	// Retail 87cbc0 sets MIN/MAG/MIP to LINEAR (2) with no anisotropic filter;
	// these defaults are that. Experimental > Image / World > Anisotropic filtering
	// swaps in the ANISOTROPY samplers below (geometry textureOptions).
	const worldSampler = created.createSampler( {
		minFilter: "linear",
		magFilter: "linear",
		mipmapFilter: "linear",
		maxAnisotropy: 1,
		addressModeU: "repeat",
		addressModeV: "repeat"
	} );
	const lightmapSampler = created.createSampler( {
		minFilter: "linear",
		magFilter: "linear",
		mipmapFilter: "linear",
		maxAnisotropy: 1,
		addressModeU: "clamp-to-edge",
		addressModeV: "clamp-to-edge"
	} );
	const anisotropicLightmapSampler = created.createSampler( {
		minFilter: "linear",
		magFilter: "linear",
		mipmapFilter: "linear",
		maxAnisotropy: ANISOTROPY,
		addressModeU: "clamp-to-edge",
		addressModeV: "clamp-to-edge"
	} );
	return {
		/*
		================
		worldSampling

		The world texture sampler for the filtering and detail options.
		================
		*/
		worldSampling( filtered: boolean, detail: number, anisotropic = false ) {
			return created.createSampler( {
				minFilter: filtered ? "linear" : "nearest",
				magFilter: filtered ? "linear" : "nearest",
				mipmapFilter: filtered ? "linear" : "nearest",
				// Anisotropy needs all-linear filters, so only the filtered
				// path takes the experimental stage; the retail value is 1.
				maxAnisotropy: filtered && anisotropic ? ANISOTROPY : 1,
				lodMinClamp: 2 - detail,
				addressModeU: "repeat",
				addressModeV: "repeat"
			} );
		},
		lightmapSampler,
		/*
		================
		lightmapSampling

		The clamped (lightmap and decal) sampler for the anisotropy stage.
		================
		*/
		lightmapSampling( anisotropic: boolean ) {
			return anisotropic ? anisotropicLightmapSampler : lightmapSampler;
		},
		sky( sceneFloat = false ) {
			if ( !sceneFloat ) return skyPipeline!;
			skyFloatPipeline ??= created.createRenderPipeline( {
				layout: "auto",
				vertex: { module: skyShader, entryPoint: "vs" },
				fragment: { module: skyShader, entryPoint: "fs", targets: [ { format: SCENE_FLOAT_FORMAT } ] },
				primitive: { topology: "triangle-list" },
				depthStencil: { format: "depth24plus", depthWriteEnabled: false, depthCompare: "always" }
			} );
			return skyFloatPipeline;
		},
		mips: () => mipPipeline!,
		image( sceneFloat = false ) {
			if ( !sceneFloat ) return pipeline!;
			imageFloatPipeline ??= created.createRenderPipeline( {
				layout: "auto",
				vertex: { module: shader, entryPoint: "vs" },
				fragment: { module: shader, entryPoint: "fs", targets: [ { format: SCENE_FLOAT_FORMAT } ] },
				primitive: { topology: "triangle-list" },
				depthStencil: { format: "depth24plus", depthWriteEnabled: false, depthCompare: "always" }
			} );
			return imageFloatPipeline;
		},
		/*
================
geometry

The geometry pipeline for state, compiled on first use. The HDR stage's
rgba16float sibling compiles lazily under the same rule.
================
		*/
		geometry( state: GeometryPipelineState, sceneFloat = false, fullscreenPreview = false ) {
			const previewCoverage = sceneFloat && fullscreenPreview;
			const key = geometryPipelineKey( state ) + (sceneFloat ? "|hdr" : "") +
				(previewCoverage ? "|preview" : "");
			let selected = geometryPipelines.get( key );
			if ( !selected ) {
				selected = created.createRenderPipeline(
					geometryDescriptor( state, sceneFloat ? SCENE_FLOAT_FORMAT : format, previewCoverage )
				);
				geometryPipelines.set( key, selected );
			}
			return selected;
		},
		sampler: sampler!,
		worldSampler,
		ready: Promise.all( [ prepared, preparedGeometry, preparedMips, preparedSky ] ).then(
			( [image, , mips, sky] ) => {
				skyPipeline = sky;
				mipPipeline = mips;
				pipeline = image;
			}
		)
	};
}
