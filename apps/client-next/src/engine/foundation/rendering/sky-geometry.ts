import type {WorldGroup} from '@/engine/contracts/scene';
import {identity} from '@/engine/foundation/rendering/world-math';
// CSkyDome_BuildGeodesicSphere: octahedron, four midpoint-normalize subdivisions, R=20000.
// Pure worker-side construction; no frame observers or GPU ownership here.
export function skyGroups(sky:{cloudTexturePublicPath?:string;sunTexturePublicPath?:string;moonTexturePublicPaths?:readonly string[];starPrimitive?:{vertices:readonly {x:number;y:number;z:number;colorArgb:number}[]}}):WorldGroup[]{
 const groups:WorldGroup[]=[];
 const add=(kind:1|2|3|4|5|6,positions:number[],indices:number[],uvs:number[]=Array(positions.length/3*2).fill(0),colors:number[]=Array(positions.length/3*4).fill(1),texture?:string,frames?:readonly string[],maskUVs?:number[])=>{
  groups.push({id:`sky:${kind}`,center:[0,0,0],radius:5000000,material:{sky:kind,texture,frames,color:[1,1,1,1],blend:true,alphaCutoff:0,doubleSided:true,unlit:true},geometry:{world:true,positions:new Float32Array(positions),normals:new Float32Array(positions.length).fill(1),uvs:new Float32Array(uvs),colors:new Float32Array(colors),maskUVs:maskUVs?new Float32Array(maskUVs):undefined,indices:new Uint32Array(indices),instances:identity(),transform:identity()}});
 };
 const v:number[][]=[[0,1,0],[0,0,1],[1,0,0],[0,0,-1],[-1,0,0],[0,-1,0]];let tris=[[0,2,1],[0,3,2],[0,4,3],[0,1,4],[5,1,2],[5,2,3],[5,3,4],[5,4,1]];
 for(let pass=0;pass<4;pass++){
  const edges=new Map<string,number>();const middle=(a:number,b:number)=>{const key=`${Math.min(a,b)}:${Math.max(a,b)}`;let i=edges.get(key);if(i!==undefined)return i;const p=v[a]!.map((x,c)=>(x+v[b]![c]!)*.5),length=Math.hypot(...p);i=v.length;v.push(p.map(x=>x/length));edges.set(key,i);return i;};
  tris=tris.flatMap(([a,b,c])=>{const ab=middle(a!,b!),bc=middle(b!,c!),ca=middle(c!,a!);return [[a!,ab,ca],[ab,bc,ca],[b!,bc,ab],[bc,c!,ca]];});
 }
 for(const kind of [1,6] as const){const map=new Map<number,number>(),p:number[]=[],colors:number[]=[],indices:number[]=[];v.forEach((point,i)=>{if(kind===1?point[1]!>=0:point[1]!*20000<=2207){map.set(i,p.length/3);p.push(...point.map(x=>x*20000));colors.push(1,1,1,kind===6&&point[1]!>0?0:1);}});for(const tri of tris)if(tri.every(i=>map.has(i)))indices.push(...tri.map(i=>map.get(i)!));add(kind,p,indices,undefined,colors);}
 if(sky.starPrimitive){
  if(sky.starPrimitive.vertices.length!==3000)throw new Error('Invalid native star field');
  const p:number[]=[],c:number[]=[],uv:number[]=[],mask:number[]=[],indices:number[]=[];
  sky.starPrimitive.vertices.forEach((v,i)=>{
   for(const corner of [[-1,-1],[1,-1],[1,1],[-1,1]]){p.push(v.x,v.y,v.z);c.push(((v.colorArgb>>>16)&255)/255,((v.colorArgb>>>8)&255)/255,(v.colorArgb&255)/255,(v.colorArgb>>>24)/255);uv.push(...corner);mask.push(i<1000?Math.floor(i/100):10,i<1000?2:1);}
   const n=i*4;indices.push(n,n+1,n+2,n,n+2,n+3);
  });
  add(2,p,indices,uv,c,undefined,undefined,mask);
 }
 for(const kind of [3,4] as const){const frames=kind===4?sky.moonTexturePublicPaths:undefined,texture=kind===3?sky.sunTexturePublicPath:frames?.[0];if(texture)add(kind,[0,-1,1,0,1,1,0,1,-1,0,-1,-1],[0,1,2,0,2,3],[0,0,1,0,1,1,0,1],undefined,texture,frames);}
 if(sky.cloudTexturePublicPath){const p:number[]=[],uv:number[]=[],indices:number[]=[];for(let z=0;z<5;z++)for(let x=0;x<5;x++){p.push((x-2)*2000000,20000,(z-2)*2000000);uv.push((x-2)*16,(z-2)*16);}for(let z=0;z<4;z++)for(let x=0;x<4;x++){const a=z*5+x;indices.push(a,a+1,a+6,a,a+6,a+5);}add(5,p,indices,uv,undefined,sky.cloudTexturePublicPath);}
 return groups.sort((a,b)=>a.material.sky!-b.material.sky!);
}
