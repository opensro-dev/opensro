/*
===========================================================================

animation-shader.ts - the skeletal animation compute pass

A workgroup owns one skeleton. All invocations reach every hierarchy
barrier. Inputs carry the CPU-resolved clip phase, never an hours-long f32
clock. The skeleton (hierarchy, rest pose, bind data) and the clip set
(keyframe tables, shared by models with the same clips) are separate
buffers (foundation/animation/gpu-animation-plan.ts); a node past the clip
set's animated nodes keeps its rest pose.

===========================================================================
*/
export const animationShader = `
struct Configuration {nodes:u32,depths:u32,joints:u32,jointAt:u32,parents:u32,rest:u32,fixed:u32,matrices:u32,depthAt:u32,clipNodes:u32,inverse:u32,padding:u32}
@group(0) @binding(0) var<storage,read> skeleton:array<f32>;
@group(0) @binding(1) var<storage,read> clips:array<f32>;
@group(0) @binding(2) var<storage,read> actors:array<vec4f>;
@group(0) @binding(3) var<storage,read_write> palettes:array<mat4x4f>;
@group(0) @binding(4) var<uniform> config:Configuration;
var<workgroup> locals:array<mat4x4f,128>;
var<workgroup> worlds:array<mat4x4f,128>;
fn v4(at:u32)->vec4f{return vec4f(skeleton[at],skeleton[at+1],skeleton[at+2],skeleton[at+3]);}
fn matrix(at:u32)->mat4x4f{return mat4x4f(v4(at),v4(at+4),v4(at+8),v4(at+12));}
fn sample(table:u32,node:u32,path:u32,time:f32,rest:vec4f)->vec4f{
 if(node>=config.clipNodes){return rest;}
 let at=table+(node*3+path)*4;let count=u32(clips[at]);if(count==0){return rest;}
 let times=u32(clips[at+1]);let values=u32(clips[at+2]);let width=select(3u,4u,path==1);
 var low=0u;var high=count;loop{if(low>=high){break;}let mid=(low+high)/2;if(clips[times+mid]<=time){low=mid+1;}else{high=mid;}}
 low=select(0u,low-1,low>0);let next=min(low+1,count-1);var f=0.;if(next!=low){f=clamp((time-clips[times+low])/(clips[times+next]-clips[times+low]),0.,1.);}
 var a=vec4f(0.);var b=vec4f(0.);for(var k=0u;k<width;k++){a[k]=clips[values+low*width+k];b[k]=clips[values+next*width+k];}
 if(clips[at+3]==1.||next==low){return a;}if(path!=1){return a*(1.-f)+b*f;}
 let d=dot(a,b);let sign=select(1.,-1.,d<0.);let angle=acos(min(1.,abs(d)));let sine=sin(angle);var left=1.-f;var right=f;
 if(sine>=0.000001){left=sin((1.-f)*angle)/sine;right=sin(f*angle)/sine;}return a*left+b*(right*sign);
}
fn compose(t:vec4f,qr:vec4f,s:vec4f)->mat4x4f{
 let q=normalize(qr);let x=q.x;let y=q.y;let z=q.z;let w=q.w;
 return mat4x4f(vec4f((1.-2.*(y*y+z*z))*s.x,2.*(x*y+z*w)*s.x,2.*(x*z-y*w)*s.x,0.),vec4f(2.*(x*y-z*w)*s.y,(1.-2.*(x*x+z*z))*s.y,2.*(y*z+x*w)*s.y,0.),vec4f(2.*(x*z+y*w)*s.z,2.*(y*z-x*w)*s.z,(1.-2.*(x*x+y*y))*s.z,0.),vec4f(t.xyz,1.));
}
@compute @workgroup_size(128) fn main(@builtin(workgroup_id) group:vec3u,@builtin(local_invocation_index) node:u32){
 let input=actors[group.x];let table=u32(clips[u32(input.y)*2+1]);
 if(node<config.nodes){let rest=config.rest+node*12;var local:mat4x4f;
  if(skeleton[config.fixed+node]>0.){local=matrix(config.matrices+node*16);}else{local=compose(sample(table,node,0,input.x,v4(rest)),sample(table,node,1,input.x,v4(rest+4)),sample(table,node,2,input.x,v4(rest+8)));}locals[node]=local;
 }
 workgroupBarrier();
 for(var depth=0u;depth<config.depths;depth++){
  if(node<config.nodes&&u32(skeleton[config.depthAt+node])==depth){let parent=i32(skeleton[config.parents+node]);if(parent<0){worlds[node]=locals[node];}else{worlds[node]=worlds[u32(parent)]*locals[node];}}
  workgroupBarrier();
 }
 for(var joint=node;joint<config.joints;joint+=128){palettes[u32(input.z)*config.joints+joint]=worlds[u32(skeleton[config.jointAt+joint])]*matrix(config.inverse+joint*16);}
}`;
