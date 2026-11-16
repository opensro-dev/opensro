import type {EntityState} from '@/engine/contracts/world';
import type {GameplayState} from '@/engine/contracts/gameplay';
// Derived indices belong to delivered state, not the render clock. Keep bounded
// key snapshots as well: tests and non-journal producers may reuse input arrays.
// No actor/effect timer or interpolated pose is cached here.
export function createCharacterStateIndex(){
 let entities:readonly EntityState[]|undefined,casts:GameplayState['casts']|undefined,vitals:GameplayState['vitals']|undefined;
 // A rebuilt index is a new map, never the old one cleared and refilled: V8
 // allocates a long-lived map's next table in the old generation, so every
 // rebuild left a table only a full collection frees. Readers take the maps
 // from update's result each frame; none keeps one across frames.
 let entitiesByGid=new Map<number,EntityState>(),castByActor=new Map<number,GameplayState['casts'][number]>(),vitalsByGid=new Map<number,GameplayState['vitals'][number]>(),castTokens=new Set<number>();
 const tokens:number[]=[];
 const value={entitiesByGid,castByActor,vitalsByGid,castTokens};
 return {update(next:readonly EntityState[],gameplay:GameplayState|null|undefined){
  if(entities!==next||next.length!==entitiesByGid.size||next.some(row=>entitiesByGid.get(row.gid)!==row)){entitiesByGid=value.entitiesByGid=new Map();for(const entity of next)entitiesByGid.set(entity.gid,entity);entities=next;}
  if(casts!==gameplay?.casts||tokens.length!==(gameplay?.casts.length??0)||gameplay?.casts.some((row,i)=>castByActor.get(row.caster)!==row||tokens[i]!==row.token)){castByActor=value.castByActor=new Map();castTokens=value.castTokens=new Set();tokens.length=0;for(const cast of gameplay?.casts??[]){if(!cast.resultOnly)castByActor.set(cast.caster,cast);castTokens.add(cast.token);tokens.push(cast.token);}casts=gameplay?.casts;}
  if(vitals!==gameplay?.vitals||(gameplay?.vitals.length??0)!==vitalsByGid.size||gameplay?.vitals.some(row=>vitalsByGid.get(row.gid)!==row)){vitalsByGid=value.vitalsByGid=new Map();for(const vital of gameplay?.vitals??[])vitalsByGid.set(vital.gid,vital);vitals=gameplay?.vitals;}
  return value;
 },reset(){entities=casts=vitals=undefined;tokens.length=0;entitiesByGid.clear();castByActor.clear();vitalsByGid.clear();castTokens.clear();}};
}
