import {visibleFrustumSphere,visibleFrustumAabb} from './world-math';
type Volume={readonly bounds?:readonly [number,number,number,number,number,number];readonly center:readonly [number,number,number];readonly radius:number};
/** Material/LOD slices of one terrain cell frequently share the exact same bound. */
export function createTerrainVisibility(){
 const shared=new Map<string,number>(),entries=new WeakMap<Volume,number>(),volumes:Volume[]=[];
 let mask=new Uint8Array(0),evaluations=0;
 return {
  admit(volume:Volume){
   if(entries.has(volume))return;
   const key=volume.bounds?'box:'+volume.bounds.join(','):'sphere:'+volume.center.join(',')+','+volume.radius;
   let entry=shared.get(key);if(entry===undefined){entry=volumes.length;volumes.push(volume);shared.set(key,entry);if(volumes.length>mask.length){const next=new Uint8Array(2**Math.ceil(Math.log2(volumes.length)));next.set(mask);mask=next;}}entries.set(volume,entry);
  },
  // Compile immutable material slices once. Render-rate consumers read numeric
  // slots instead of repeating WeakMap lookups for every material/LOD range.
  indices(values:readonly Volume[]){return Uint32Array.from(values,volume=>{const index=entries.get(volume);if(index===undefined)throw Error('Terrain volume was not admitted');return index;});},
  begin(frustum:Float64Array){
   for(let i=0;i<volumes.length;i++){const volume=volumes[i]!,center=volume.center;mask[i]=Number(volume.bounds?visibleFrustumAabb(frustum,volume.bounds):visibleFrustumSphere(frustum,center[0],center[1],center[2],volume.radius));}
   evaluations=volumes.length;return mask;
  },
  visible(volume:Volume){
   const entry=entries.get(volume);if(entry===undefined)throw Error('Terrain volume was not admitted');return mask[entry]===1;
  },
  stats:()=>({volumes:shared.size,evaluations})
 };
}
