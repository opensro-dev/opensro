import type {AssetOwner} from '@/engine/contracts/assets';
import {minimapNpcPositions} from '@/engine/foundation/ui/minimap-catalog';
import {minimapDungeons,minimapDungeonRooms,minimapArt} from '@/engine/foundation/ui/minimap-tiles';
// Catalog jobs share the UI lifetime and the existing asset scheduler.
export function createMinimapResources(assets:Pick<AssetOwner,'available'|'request'|'take'|'cancel'>,base:string){
 const dungeon=createDungeonResources(assets,base);
 let state:{kind:'idle'}|{kind:'loading';id:number}|{kind:'ready';positions:ReturnType<typeof minimapNpcPositions>}|{kind:'failed';error:string}|{kind:'disposed'}={kind:'idle'};
 return {step(needed:boolean,worldVisible=false){const changed=dungeon.step(worldVisible);if(state.kind==='idle'&&needed&&assets.available()>0){state={kind:'loading',id:assets.request(new URL('/assets/data/npcpos.json',base).href,4<<20)};}
  if(state.kind==='loading'){const result=assets.take(state.id);if(result){try{if(result.kind!=='bytes')throw Error(result.kind==='error'?result.error:'Invalid minimap catalog response');state={kind:'ready',positions:minimapNpcPositions(JSON.parse(new TextDecoder('utf-8',{fatal:true}).decode(result.buffer)))};}catch(error){state={kind:'failed',error:String(error)};}return true;}}return changed;},
 positions:()=>state.kind==='ready'?state.positions:undefined,dungeons:dungeon.data,rooms:dungeon.rooms,art:dungeon.art,error:()=>state.kind==='failed'?state.error:dungeon.error(),
 dispose(){dungeon.dispose();if(state.kind==='loading')assets.cancel(state.id);state={kind:'disposed'};}};
}
function createDungeonResources(assets:Pick<AssetOwner,'available'|'request'|'take'|'cancel'>,base:string){
 let state:'idle'|'loading'|'ready'|'failed'|'disposed'='idle',id=0,data:ReturnType<typeof minimapDungeons>|undefined,error:string|null=null;
 let art:ReturnType<typeof minimapArt>|undefined,rooms:ReturnType<typeof minimapDungeonRooms>|undefined;
 return {step(needed:boolean){if(state==='idle'&&needed&&assets.available()>0){id=assets.request(new URL('/assets/data/mission-dungeon-minimap.json',base).href,4<<20);state='loading';}
 if(state==='loading'){const result=assets.take(id);if(result){try{if(result.kind!=='bytes')throw Error(result.kind==='error'?result.error:'Invalid minimap response');const value=JSON.parse(new TextDecoder('utf-8',{fatal:true}).decode(result.buffer)),nextArt=minimapArt(value),nextData=minimapDungeons(value),nextRooms=minimapDungeonRooms(value);art=nextArt;data=nextData;rooms=nextRooms;state='ready';}catch(e){error=String(e);state='failed';}return true;}}return false;},
 data:()=>data,rooms:()=>rooms,art:()=>art,error:()=>error,dispose(){if(state==='loading')assets.cancel(id);state='disposed';data=undefined;rooms=undefined;art=undefined;}};
}
