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

// Experimental video stages (Experimental > Video). Each is off by
// default, which is the native frame; env.stages carries the switches
// (x height fog, y water reflection, z garment sheen).
//
// Height fog - a deliberate deviation from the retail D3DFOG_LINEAR the
// native client authored (sub_4dc920 start/end): the same start/end
// uniforms drive an exp2 falloff with a height term and a horizon tint,
// which keeps the mid-range clear, softens the horizon and lets peaks
// rise out of the haze. Off, the retail linear ramp and colours stand.
const FOG_EXP2_REACH = 2.5; // exp2 factor reaches 1 - 1/255 at the fog end
const FOG_HEIGHT_FALLOFF = 0.004; // fog density e-folds 250 m above the eye
const FOG_SKY_TINT = 0.3; // global fog colour blended toward the horizon colour

// Water shading - the 30 authored wave frames (water1XX, x/y slope
// offsets around 0.5) ship and animate as texture array layers but the
// flat unlit water path never sampled them. Fresnel against a
// horizon-tinted sky reflection now uses them: the surface is horizontal,
// so the eye height over view depth carries the view angle and no camera
// uniform is needed. The flat authored look is this block.
const WATER_FRESNEL = 0.02; // F0 base reflectance of a water surface
const WATER_WAVE_SLOPE = 1; // wave-frame slope weight in the reflection lookup

// Garment sheen - opaque DXT3 character parts author gloss in the alpha
// channel (jmxAssetIO: CPrimMtrl bit 0x200 off means alpha is not
// coverage). A Blinn-Phong term against the fixed 45-degree sun the
// diffuse lighting already uses; the view vector is the reversed camera
// forward (surface to eye), exact at the frame centre.
const SHEEN_POWER = 24; // highlight tightness
const SHEEN_STRENGTH = 0.5; // gloss gain on the lit term

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
	const environmentStruct =
		`struct Environment {zenith:vec4f,horizon:vec4f,diffuse:vec4f,ambient:vec4f,forward:vec4f,right:vec4f,up:vec4f,fog:vec4f,settings:vec4f,water:vec4f,shadow:vec4f,scatter:vec4f,skyTime:vec4f,sun:vec4f,lunar:vec4f,stars:array<vec4f,3>,terrainFog:vec4f,terrainBand:vec4f,reflection:vec4f,stages:vec4f}`;
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
struct SkinVertex {joints:vec4u,weights:vec4f}
@group(0) @binding(6) var<storage,read> skinVertices:array<SkinVertex>;
@group(0) @binding(7) var<storage,read> bones:array<mat4x4f>;
@group(0) @binding(8) var sphereMap:texture_2d_array<f32>;
struct Out {@builtin(position) position:vec4f,@location(0) uv:vec2f,@location(1) normal:vec3f,@location(2) color:vec4f,@location(3) maskUV:vec2f,@location(4) viewZ:f32,@location(5) objectLighting:vec3f,@location(6) @interpolate(flat) opacity:f32,@location(7) worldXZ:vec2f,@location(8) @interpolate(flat) materialTint:vec3f,@location(9) sphereUV:vec2f,@location(10) equipmentUV:vec2f,@location(11) worldY:f32}
@vertex fn vs(@location(0) position:vec3f,@location(1) normal:vec3f,@location(2) uv:vec2f,@location(3) color:vec4f,@location(4) maskUV:vec2f,@builtin(instance_index) i:u32,@builtin(vertex_index) vertex:u32)->Out {
 var o:Out;let instance=instances[i].matrix;o.opacity=instances[i].opacity.x;var p=vec4f(position,1);var n=vec4f(normal,0);
 if(material.skin.x!=0){let v=skinVertices[vertex];let base=select(select(i*u32(abs(material.skin.x)),0u,material.skin.x<0),u32(instances[i].opacity.y),instances[i].opacity.z>0);let skin=bones[base+v.joints.x]*v.weights.x+bones[base+v.joints.y]*v.weights.y+bones[base+v.joints.z]*v.weights.z+bones[base+v.joints.w]*v.weights.w;p=skin*p;n=skin*n;}
 o.worldXZ=(instance*p).xz;o.worldY=(instance*p).y;o.position=transform*instance*p;o.normal=(instance*n).xyz;let movingUV=vec2f(dot(vec3f(uv,1),material.uvU.xyz),dot(vec3f(uv,1),material.uvV.xyz));o.uv=(movingUV*material.window.xy+material.window.zw)*instances[i].window.xy+instances[i].window.zw;o.materialTint=select(vec3f(1),instances[i].color.rgb,material.policy.w>0.5);o.color=color*select(instances[i].color,vec4f(1,1,1,instances[i].color.a),material.policy.w>0.5);o.maskUV=maskUV;o.viewZ=o.position.w;
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
 // A91970 replaces BMT diffuse and actor ambient during SCT_MAT.
 let diffuseFactor=select(material.color.rgb,o.materialTint,material.policy.w>0.5);
 let ambientFactor=select(material.ambient.rgb,o.materialTint,material.policy.w>0.5);
 var illumination=diffuseFactor*env.diffuse.rgb*max(0.0,dot(lightingNormal,vec3f(0.70710678,0.70710678,0)))+env.ambient.rgb*ambientFactor*material.lighting.x;
 let light=instances[i];
 if(nativeCharacterLighting&&(any(light.pointAmbient.rgb!=vec3f(0))||any(light.pointDiffuse.rgb!=vec3f(0)))){
  let localLight=transpose(cofactors)*(light.pointPosition.xyz-instance[3].xyz)/select(1.0,determinant,determinant!=0.0);
  let delta=localLight-p.xyz;let distance=length(delta);let normalObject=select(n.xyz,normalize(n.xyz),material.skin.x!=0);
  let lambert=select(0.0,max(0.0,dot(normalObject,delta/max(distance,0.000001)))/max(light.pointPosition.w*distance,0.000001),distance>0.0);
  illumination+=light.pointAmbient.rgb+diffuseFactor*light.pointDiffuse.rgb*lambert;
 }
 o.objectLighting=clamp(illumination,vec3f(0),vec3f(1));
 if(material.skin.w>0){
  let k=material.skin.w;var skyPosition=position;let angle=(env.skyTime.x-0.25)*6.28318530718;let sunPosition=vec3f(cos(angle),sin(angle),0)*20000.0;
  if(k==3||k==4){var a=angle;if(k==4){let w=fract(env.skyTime.x+0.25);a=(0.25+(w-0.25)*1.2)*6.28318530718;}
   let center=vec3f(cos(a),sin(a),0)*20000.0;skyPosition=center+vec3f(-position.y*sin(a),position.y*cos(a),position.z)*select(666.666687,1000.0,k==4);
  }
  let z=dot(skyPosition,env.forward.xyz);o.position=vec4f(dot(skyPosition,env.right.xyz)/dot(env.right.xyz,env.right.xyz),dot(skyPosition,env.up.xyz)/dot(env.up.xyz,env.up.xyz),z,z);o.viewZ=z;
  if(k==1){let base=mix(env.horizon.rgb,env.zenith.rgb,clamp(position.y/5000.0*env.sun.w,0,1));o.color=vec4f(mix(env.scatter.rgb,base,min(1.0,distance(position,sunPosition)/env.scatter.w)),1);}
  if(k==2){let batch=u32(maskUV.x);let alpha=env.stars[batch/4u][batch%4u];o.color.a*=f32(u32(max(0.0,alpha*env.skyTime.w))&255u)/255.0;o.position=vec4f(o.position.xy+uv*maskUV.y*env.lunar.yz*z,o.position.zw);}
  if(k==5){o.uv=uv+vec2f(env.skyTime.y);}
 }
 return o;
}
@fragment fn fs(input:Out)->@location(0) vec4f {
 var reflected=vec3f(0);var equipment=vec3f(0);
 if(material.equipmentColor.w>0.0){equipment=textureSample(sphereMap,textureSampler,input.equipmentUV,0).rgb;}
 // Keep implicit-derivative sampling behind uniform gates, before varying exits.
 if(material.reflection.x>0.5&&env.reflection.w>0.5){reflected=textureSample(sphereMap,textureSampler,input.sphereUV,0).rgb;}
 let layer=select(u32(env.settings.z),u32(env.lunar.x),material.skin.w==4.0);let tex=textureSample(albedo,textureSampler,input.uv,i32(layer%textureNumLayers(albedo)));
 if(material.skin.w>0){let k=material.skin.w;
  if(k==1||k==2){return input.color;}
  if(k==3){return vec4f(clamp(env.sun.rgb,vec3f(0),vec3f(1)),tex.a*select(0.0,1.0,env.skyTime.x>=0.25&&env.skyTime.x<=0.875));}
  if(k==4){return vec4f(tex.rgb,tex.a*select(0.0,1.0,(env.skyTime.x>0.75||env.skyTime.x<0.5)&&env.lunar.x<29.0));}
  if(k==5){return vec4f(mix(clamp(env.scatter.rgb,vec3f(0),vec3f(1))*tex.rgb,env.fog.rgb,clamp((input.viewZ-90000.0)/110000.0,0,1)),tex.a*clamp(env.skyTime.z,0,1));}
  return vec4f(env.fog.rgb,input.color.a);
 }
var light=vec4f(1);if(material.skin.y>0.5){light=textureSampleBias(albedo,textureSampler,input.uv,0,-0.5);}
 // Retail 8ABFD0: cells outside the detailed band use untextured linear fog.
 let cellDelta=floor(input.worldXZ/320.0)-env.terrainBand.xy;
 let distant=env.terrainBand.w>0.5&&dot(cellDelta,cellDelta)>env.terrainBand.z;
 if(distant&&material.skin.y>0.5){discard;}
 if(distant&&material.options.z>0.5){return vec4f(env.fog.rgb,1);}
let mask=mix(mix(input.color.x,input.color.y,input.maskUV.x),mix(input.color.z,input.color.w,input.maskUV.x),input.maskUV.y);var color=vec4f(tex.rgb,select(tex.a,1.0,material.policy.z>0.5))*material.color*select(input.color,vec4f(1,1,1,mask),material.options.z>0.5);
 let illumination=clamp(select(material.color.rgb,input.materialTint,material.policy.w>0.5)*env.diffuse.rgb*max(0.0,dot(normalize(input.normal),vec3f(0.70710678,0.70710678,0)))+env.ambient.rgb*select(material.ambient.rgb,input.materialTint,material.policy.w>0.5)*material.lighting.x,vec3f(0),vec3f(1));
 let surfaceLight=select(illumination,input.objectLighting,material.lighting.y>0.5);
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
 // Authored garment gloss from the opaque DXT3 alpha (experimental).
 if(material.reflection.z>0.5&&env.stages.z>0.5&&input.opacity>=1.0){
  let halfVec=normalize(vec3f(0.70710678,0.70710678,0)-env.forward.xyz);
  let gloss=pow(max(0.0,dot(normalize(input.normal),halfVec)),${SHEEN_POWER});
  lit=clamp(lit+tex.a*gloss*${SHEEN_STRENGTH}*env.diffuse.rgb,vec3f(0),vec3f(1));
 }
 let animated=material.skin.z>0.5;
 // Fresnel-weighted sky reflection on animated water. The reflected ray
 // sees zenith when the eye looks down, horizon when grazing; the wave
 // frame's slope shimmers the lookup between the two.
 var waterShading=clamp(env.water.rgb,vec3f(0),vec3f(1));
 if(animated&&env.stages.y>0.5){
  let slope=(tex.rg-vec2f(0.5))*${WATER_WAVE_SLOPE};
  let cosTheta=clamp((env.settings.w-input.worldY)/max(input.viewZ,1.0),0.05,1.0);
  let fresnel=${WATER_FRESNEL}+(1.0-${WATER_FRESNEL})*pow(1.0-cosTheta,5.0);
  let skyColor=mix(env.horizon.rgb,env.zenith.rgb,clamp(cosTheta+slope.y,0.0,1.0));
  waterShading=clamp(env.water.rgb,vec3f(0),vec3f(1))*(1.0-fresnel)+skyColor*fresnel;
 }
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
 if(material.skin.y>0.5){return vec4f(mix(clamp(light.rgb+env.shadow.rgb,vec3f(0),vec3f(1)),terrainFogColor,fog),1);}
 return vec4f(mix(lit*select(vec3f(1),waterShading,animated),select(fogColor,terrainFogColor,material.options.z>0.5),fog),select(select(color.a,clamp(input.color.a,0,1),animated),select(select(1.0,color.a,material.ambient.w>0.5),fadeAlpha,fading),material.lighting.z>0.5));
}`
	} );
	/*
	================
	geometryDescriptor

	The geometry pipeline for one state: the material's D3D blend pair (or
	none), culling, depth writes and depth test.
	================
	*/
	const geometryDescriptor = ( state: GeometryPipelineState ): GPURenderPipelineDescriptor => ({
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
			entryPoint: "fs",
			targets: [ { format, ...(state.blend ? { blend: blendState( state.blend ) } : {}) } ]
		},
		primitive: { topology: "triangle-list", cullMode: state.cull ? "back" : "none", frontFace: "cw" },
		depthStencil: {
			format: "depth24plus",
			depthWriteEnabled: state.depthWrite,
			depthCompare: state.depthCompare
		}
	});
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
	// these defaults are that. Experimental > Video > Anisotropic filtering
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
		sky: () => skyPipeline!,
		mips: () => mipPipeline!,
		image: () => pipeline!,
		/*
		================
		geometry

		The geometry pipeline for state, compiled on first use.
		================
		*/
		geometry( state: GeometryPipelineState ) {
			const key = geometryPipelineKey( state );
			let pipeline = geometryPipelines.get( key );
			if ( !pipeline ) {
				pipeline = created.createRenderPipeline( geometryDescriptor( state ) );
				geometryPipelines.set( key, pipeline );
			}
			return pipeline;
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
