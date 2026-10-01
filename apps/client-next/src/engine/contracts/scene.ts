import type {Geometry} from "./geometry";
export interface WorldMaterial {
 readonly deferredParticle?:boolean;
 readonly environmentReflection?:boolean;
 /** D3DCMPFUNC, default GREATEREQUAL. */
 readonly alphaCompare?:1|2|3|4|5|6|7|8;
 readonly textureAlphaSquared?:boolean;
 readonly colorTimeline?:import("@/engine/foundation/rendering/material-timeline").MaterialTimeline;
 readonly uvAtlas?:import("@/engine/foundation/rendering/texture-atlas").TextureAtlas;
 readonly surfaceAlpha?:boolean;
 readonly depthWrite?:boolean;
 /** Native CRTModTexAni mode 1: rates for m00,m01,m10,m11,m20,m21. */
 readonly uvVelocity?:readonly [number,number,number,number,number,number];
 readonly multiplyAddBlend?:boolean;readonly sourceColorBlend?:boolean;readonly inverseSourceColorBlend?:boolean;
 readonly instanceMaterialTint?:boolean;
 readonly decal?:boolean;
 /** SWorld pooled terrain decal pass: source alpha blend, no depth test/write. */
 readonly groundDecal?:boolean;
 readonly ambient?:readonly [number,number,number];
 readonly textureAlpha?:boolean;
 readonly fog?:{readonly color:number;readonly nearPlane:number;readonly farPlane:number;readonly intensity:number};
 readonly fogDisabled?:boolean;readonly instanceFade?:boolean;readonly objectFade?:boolean;readonly fadeAlphaOnly?:boolean;
 readonly color:readonly [number,number,number,number];
 readonly texture?:string;readonly frames?:readonly string[];
 readonly alphaCutoff:number;
 readonly sharedPose?:boolean;readonly sky?:1|2|3|4|5|6;readonly lightmap?:boolean;readonly water?:boolean;
 readonly blend:boolean;readonly additive?:boolean;
 readonly doubleSided:boolean;
 // Presence selects native object vertex lighting; scalar multiplies ambient only.
 // On this path unlit means native NOLIGHT (white), not material diffuse tint.
 readonly objectLight?:number;
 readonly stageFactor?:number;readonly unlit?:boolean;readonly terrain?:boolean;readonly order?:number;
 /** Native stage-0 colour/alpha ops (effects, CEFEffect_Render B153A0). */
 readonly textureStage?:import("@/engine/foundation/rendering/texture-stage").TextureStage;
}
export interface TerrainRange {/** Admission-owned [vertex index, 17x17 height index] pairs. */ readonly seamVertices?:Uint32Array;readonly bounds?:readonly [number,number,number,number,number,number];readonly water?:{readonly type:number;readonly waveType:number;readonly height:number};readonly cell:readonly [number,number];readonly lod:number;readonly indexStart:number;readonly indexCount:number;readonly vertexStart:number;readonly vertexCount:number;readonly center:readonly [number,number,number];readonly radius:number;readonly heights:readonly number[];}
export interface WorldGroup {
 readonly dungeonBlock?:number;
 readonly collision?:readonly {readonly instance:number;readonly object:string;readonly order:number;readonly indexStart:number;readonly indexCount:number}[];
 // Native object batches traverse a material set in index order (0xA5C510).
 readonly materialOrder?:{readonly set:string;readonly index:number};
 readonly visibility?:readonly {readonly id:string;readonly radius:number;readonly range:number;readonly sceneryRange?:boolean;readonly cells:readonly (readonly [number,number])[];readonly cellRadius:number}[];
 readonly animation?:{readonly model:string;readonly primitive:number;readonly clip:string};
 readonly instanceRadius?:number;
 readonly ranges?:readonly TerrainRange[];
 readonly id:string;
 readonly geometry:Geometry;
 readonly material:WorldMaterial;
 readonly center:readonly [number,number,number];readonly radius:number;
 readonly cell?:readonly [number,number];readonly lod?:number;
}
export interface EnvironmentTrack {readonly t:number;readonly r?:number;readonly g?:number;readonly b?:number;readonly value?:number;}
export interface WorldEnvironment {readonly startTimeOfDay:number;readonly ratePerSecond:number;readonly tracks:Readonly<Record<string,readonly EnvironmentTrack[]>>;}
export interface WorldScene {readonly scenery?:readonly import("./scenery").SceneryEmitter[];readonly soundTerrain?:readonly import("@/engine/foundation/audio/terrain-sounds").SoundTerrain[];readonly dungeonVisibility?:readonly (readonly number[])[];readonly flareTextures?:readonly string[];readonly starRandomState?:number;readonly terrainDetail?:"full"|"distance";readonly residency?:"frontend";readonly models?:Readonly<Record<string,import("./character").CharacterModel>>;readonly environment?:WorldEnvironment;readonly id:string;readonly originRegion:number;readonly groups:readonly WorldGroup[];readonly warnings:readonly string[];}
export interface WorldCamera {readonly dungeonBlock?:number;readonly follow?:{readonly yaw:number;readonly pitch:number;readonly distance:number;readonly height?:number;readonly mounted?:boolean;readonly offset?:readonly [number,number]};readonly up?:readonly [number,number,number];readonly originRegion?:number;readonly eye:readonly [number,number,number];readonly target:readonly [number,number,number];readonly fov:number;readonly near:number;readonly far:number;}
export interface WorldRenderStats {readonly pendingGroups:number;readonly sceneId:string|null;readonly residentGroups:number;readonly visibleGroups:number;readonly triangles:number;readonly pendingTextures:number;readonly bundleRebuilds:number;}

export interface FollowCameraTarget {readonly pose:import("./gameplay").Pose;readonly height:number;readonly mounted:boolean;}
