import {test} from 'node:test';
import assert from 'node:assert/strict';
import {readFile,mkdir,writeFile} from 'node:fs/promises';
import {launchProbeBrowser} from '../../../../scripts/lib/probeBrowser.mjs';
import {CLIENT_NEXT_BASE_URL} from '../../../../scripts/lib/probeEndpoints.mjs';
import {holdProbeRuntime} from './helpers/hold-runtime.mjs';

// Produce this artifact with TestAttachedEffectAuthenticatedTransport and
// SRO_EFFECT_FIXTURE_OUT. Missing transport evidence must fail this check.
test('authenticated server effect bytes survive browser decode, restoration and GPU teardown',{timeout:90000},async()=>{
 const fixture=JSON.parse(await readFile('temp/artifacts/effect-wire.json','utf8'));
 const {browser,page}=await launchProbeBrowser();
 try{
  await holdProbeRuntime(page);await page.goto(CLIENT_NEXT_BASE_URL);
  const result=await page.evaluate(async fixture=>{
   const {createAssets}=await import('/src/engine/runtime/assets/assets.ts');
   const {assetRequestBudget}=await import('/src/engine/foundation/assets/asset-budget.ts');
   const {createRenderer}=await import('/src/engine/runtime/renderer/renderer.ts');
   const {createWorldCore}=await import('/src/engine/runtime/simulation/worker/session/world/core.ts');
   const {createCharacterEffects}=await import('/src/engine/runtime/characters/effects/effects.ts');
   const {createPresentationRandom}=await import('/src/engine/runtime/random/random.ts');
   const assets=createAssets(),canvas=document.createElement('canvas'),renderer=createRenderer(canvas),core=createWorldCore(()=>{});
   const effects=createCharacterEffects(assets,location.origin,()=>{},createPresentationRandom(1)),pending=new Map(),resident=new Set(),images={};
   const path='/assets/char/china/chinaman_adventurer.glb',pose={regionId:257,x:0,y:0,z:0,yaw:0};
   const actor={gid:fixture.gid,model:path,pose,clip:'stand',time:0,loop:true,scale:1,height:18};
   const entities=[{gid:fixture.gid,kind:'local-player',regionId:257,x:0,y:0,z:0,heading:0}];
   const base={protocolVersion:2,nativeResult:1,refObjSnapshot:[],character:{name:fixture.name,skills:[]},refSkillSnapshot:[fixture.reference],localPlayerEntry:{modelRef:1907,startProfile:{regionId:257,x:0,y:0,z:0,angle:0},spawnSkills:[]}};
   const requested=new Set();
   const drain=()=>{core.step(0,false);const b=core.take();if(b)core.ack(b.sequence);return b?.events.findLast(e=>e.kind==='gameplay')?.state;};
   const enter=skills=>{core.bootstrap({...base,localPlayerEntry:{...base.localPlayerEntry,spawnSkills:skills}});drain();const payload=new Uint8Array(8);new DataView(payload.buffer).setUint32(0,fixture.gid,true);core.receive({opcode:0x32a6,payload},1000);return drain();};
   const receive=frame=>{core.receive({opcode:frame.opcode,payload:Uint8Array.from(atob(frame.payload),c=>c.charCodeAt(0))},1000);return drain();};
   function ready(path){
    requested.add(path);
    if(resident.has(path))return true;
    const id=pending.get(path);
    if(id!==undefined){const result=assets.take(id);if(result){pending.delete(path);if(result.kind!=='character')throw Error(result.error??'Unexpected model result');renderer.setCharacterModel(path,result.model,result.images);resident.add(path);return true;}}
    else if(assets.available()>0)pending.set(path,assets.request(new URL(path,location.origin).href,assetRequestBudget(path.includes('#')?'effect':'character'),path.includes('#')?'effect':'character'));
    return false;
   }
   async function sample(name,game,time,expected){
    const deadline=performance.now()+30000;let visuals=[];
    do{requested.clear();ready(path);visuals=effects.step(entities,game,time,ready,()=>1,[],undefined,[actor]);if(effects.error())throw Error(effects.error());if(visuals.some(actor=>!requested.has(actor.model)))throw Error(name+': live attached model lost residency renewal');if(performance.now()>deadline)throw Error(name+' admission timeout');await new Promise(requestAnimationFrame);}while(!resident.has(path)||(expected&&visuals.length===0));
    renderer.setCharacterActors([actor,...visuals]);
    for(let i=0;i<8;i++){renderer.frame({width:256,height:256},0);if(renderer.error())throw Error(renderer.error());await new Promise(requestAnimationFrame);}
    const copy=document.createElement('canvas');copy.width=copy.height=256;const ctx=copy.getContext('2d');ctx.drawImage(canvas,0,0);images[name]=copy.toDataURL();return {count:visuals.length,pixels:Array.from(ctx.getImageData(0,0,256,256).data)};
   }
   try{
    const rgb=[{t:0,r:.5,g:.5,b:.5}];renderer.setWorld({id:'attached-effect-wire',originRegion:257,groups:[],warnings:[],environment:{startTimeOfDay:.5,ratePerSecond:0,tracks:{color0xf0:rgb,color0x124:rgb}}});renderer.setWorldCamera({eye:[0,10,-35],target:[0,10,0],originRegion:257,fov:1,near:1,far:500});
    let game=enter([]);const before=await sample('before',game,1,false);
    game=receive(fixture.apply);const applied=await sample('applied',game,2,true),loop=await sample('loop',game,4,true);
    effects.reset();core.clear();game=enter(fixture.entry);const restored=await sample('restored',game,5,true),remaining=game.attachedEffects[0].remainingMs;
    game=receive(fixture.end);await sample('stop',game,6,false);const ended=await sample('ended',game,20,false);
    const difference=(a,b)=>a.pixels.reduce((sum,n,i)=>sum+(i%4===3?0:Math.abs(n-b.pixels[i])),0);
    return {images,counts:{applied:applied.count,loop:loop.count,restored:restored.count,ended:ended.count},remaining,visibleDifference:difference(before,loop),settledDifference:difference(before,ended)};
   }finally{effects.dispose();core.dispose();renderer.dispose();assets.dispose();}
  },fixture);
  await mkdir('temp/artifacts/effect-lifecycle',{recursive:true});
  for(const [name,png]of Object.entries(result.images))await writeFile('temp/artifacts/effect-lifecycle/'+name+'.png',Buffer.from(png.split(',')[1],'base64'));
  const {images,...metrics}=result;await writeFile('temp/artifacts/effect-lifecycle/metrics.json',JSON.stringify(metrics,null,2));
  assert.ok(result.visibleDifference>1000,'real looping effect must change rendered pixels');assert.equal(result.counts.ended,0);assert.equal(result.remaining,14000);assert.equal(result.settledDifference,0);
 }catch(error){
  await mkdir('temp/artifacts/effect-lifecycle',{recursive:true});
  await writeFile('temp/artifacts/effect-lifecycle/failure.txt',String(error.stack??error));
  throw error;
 }finally{await browser.close();}
});
