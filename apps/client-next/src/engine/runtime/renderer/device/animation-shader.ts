/*
===========================================================================

animation-shader.ts - the skeletal animation compute pass

A workgroup owns one skeleton. All invocations reach every hierarchy
barrier. Inputs carry the CPU-resolved clip phase, never an hours-long f32
clock. The skeleton (hierarchy, rest pose, bind data) and the clip set
(keyframe tables, shared by models with the same clips) are separate
buffers (foundation/animation/gpu-animation-plan.ts); a node past the clip
set's animated nodes keeps its rest pose.

Each skeleton reads two input records (CharacterGpuSample): the first
clip's phase, clip, palette slot and mix share, then the second clip's
phase and clip (-1 for none). Where both clips animate a node path the
pass mixes them by share (lerp, slerp for rotation) as the CPU mixer does;
where one does, that one; where neither does, the rest pose.

Rotations interpolate with polynomial arccos and sine (under 1e-7 on
[0, 1] and [0, pi/2]): WGSL lets acos and sin be off by 2^-11, which a
key-to-key slerp hides but a mix of two poses, whose angle is large, does
not (5e-4 against the CPU palette, gpu-animation.test.mjs).

The pass also writes every node's global matrix (no inverse bind: the
CPU pose's globals, animation-pose.ts socket) at the sample's palette slot,
so an attachment or emitter can read its holder's socket on the GPU
instead of settling the socket's ancestor chain on the CPU (#4692). Only a
stream a consumer asked for writes them (config.globals); the rest bind a
placeholder and skip the write.

===========================================================================
*/
export const animationShader = `
struct Configuration {nodes:u32,depths:u32,joints:u32,jointAt:u32,parents:u32,rest:u32,fixed:u32,matrices:u32,depthAt:u32,clipNodes:u32,inverse:u32,globals:u32}
@group(0) @binding(0) var<storage,read> skeleton:array<f32>;
@group(0) @binding(1) var<storage,read> clips:array<f32>;
@group(0) @binding(2) var<storage,read> actors:array<vec4f>;
@group(0) @binding(3) var<storage,read_write> palettes:array<mat4x4f>;
@group(0) @binding(4) var<uniform> config:Configuration;
@group(0) @binding(5) var<storage,read_write> globals:array<mat4x4f>;
var<workgroup> locals:array<mat4x4f,128>;
var<workgroup> worlds:array<mat4x4f,128>;
fn v4(at:u32)->vec4f{return vec4f(skeleton[at],skeleton[at+1],skeleton[at+2],skeleton[at+3]);}
fn matrix(at:u32)->mat4x4f{return mat4x4f(v4(at),v4(at+4),v4(at+8),v4(at+12));}
fn animates(table:u32,node:u32,path:u32)->bool{return node<config.clipNodes&&clips[table+(node*3+path)*4]>0.;}
fn arccos(x:f32)->f32{return sqrt(1.-x)*(1.5707963050+x*(-0.2145988016+x*(0.0889789874+x*(-0.0501743046+x*(0.0308918810+x*(-0.0170881256+x*(0.0066700901+x*-0.0012624911)))))));}
fn sine(x:f32)->f32{let x2=x*x;return x*(1.+x2*(-1./6.+x2*(1./120.+x2*(-1./5040.+x2*(1./362880.+x2*(-1./39916800.))))));}
fn slerp(a:vec4f,b:vec4f,f:f32)->vec4f{
 let d=dot(a,b);let sign=select(1.,-1.,d<0.);let angle=arccos(min(1.,abs(d)));let s=sine(angle);var left=1.-f;var right=f;
 if(s>=0.000001){left=sine((1.-f)*angle)/s;right=sine(f*angle)/s;}return a*left+b*(right*sign);
}
fn sample(table:u32,node:u32,path:u32,time:f32,rest:vec4f)->vec4f{
 if(!animates(table,node,path)){return rest;}
 let at=table+(node*3+path)*4;let count=u32(clips[at]);
 let times=u32(clips[at+1]);let values=u32(clips[at+2]);let width=select(3u,4u,path==1);
 var low=0u;var high=count;loop{if(low>=high){break;}let mid=(low+high)/2;if(clips[times+mid]<=time){low=mid+1;}else{high=mid;}}
 low=select(0u,low-1,low>0);let next=min(low+1,count-1);var f=0.;if(next!=low){f=clamp((time-clips[times+low])/(clips[times+next]-clips[times+low]),0.,1.);}
 var a=vec4f(0.);var b=vec4f(0.);for(var k=0u;k<width;k++){a[k]=clips[values+low*width+k];b[k]=clips[values+next*width+k];}
 if(clips[at+3]==1.||next==low){return a;}if(path!=1){return a*(1.-f)+b*f;}return slerp(a,b,f);
}
fn mixed(first:u32,second:i32,node:u32,path:u32,input:vec4f,next:vec4f,rest:vec4f)->vec4f{
 let a=sample(first,node,path,input.x,rest);if(second<0){return a;}
 let table=u32(second);if(!animates(table,node,path)){return a;}
 let b=sample(table,node,path,next.x,rest);if(!animates(first,node,path)){return b;}
 if(path==1){return slerp(a,b,input.w);}return a+(b-a)*input.w;
}
fn compose(t:vec4f,qr:vec4f,s:vec4f)->mat4x4f{
 let q=normalize(qr);let x=q.x;let y=q.y;let z=q.z;let w=q.w;
 return mat4x4f(vec4f((1.-2.*(y*y+z*z))*s.x,2.*(x*y+z*w)*s.x,2.*(x*z-y*w)*s.x,0.),vec4f(2.*(x*y-z*w)*s.y,(1.-2.*(x*x+z*z))*s.y,2.*(y*z+x*w)*s.y,0.),vec4f(2.*(x*z+y*w)*s.z,2.*(y*z-x*w)*s.z,(1.-2.*(x*x+y*y))*s.z,0.),vec4f(t.xyz,1.));
}
@compute @workgroup_size(128) fn main(@builtin(workgroup_id) group:vec3u,@builtin(local_invocation_index) node:u32){
 let input=actors[group.x*2];let next=actors[group.x*2+1];let table=u32(clips[u32(input.y)*2+1]);
 var second=-1;if(next.y>=0.){second=i32(clips[u32(next.y)*2+1]);}
 if(node<config.nodes){let rest=config.rest+node*12;var local:mat4x4f;
  if(skeleton[config.fixed+node]>0.){local=matrix(config.matrices+node*16);}else{local=compose(mixed(table,second,node,0,input,next,v4(rest)),mixed(table,second,node,1,input,next,v4(rest+4)),mixed(table,second,node,2,input,next,v4(rest+8)));}locals[node]=local;
 }
 workgroupBarrier();
 for(var depth=0u;depth<config.depths;depth++){
  if(node<config.nodes&&u32(skeleton[config.depthAt+node])==depth){let parent=i32(skeleton[config.parents+node]);if(parent<0){worlds[node]=locals[node];}else{worlds[node]=worlds[u32(parent)]*locals[node];}}
  workgroupBarrier();
 }
 if(config.globals>0u&&node<config.nodes){globals[u32(input.z)*config.nodes+node]=worlds[node];}
 for(var joint=node;joint<config.joints;joint+=128){palettes[u32(input.z)*config.joints+joint]=worlds[u32(skeleton[config.jointAt+joint])]*matrix(config.inverse+joint*16);}
}`;
