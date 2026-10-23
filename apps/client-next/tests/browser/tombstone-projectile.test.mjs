import {test} from 'node:test';
import assert from 'node:assert/strict';
import {mkdir,readFile,writeFile} from 'node:fs/promises';
import {launchProbeBrowser} from '../../../../scripts/lib/probeBrowser.mjs';
import {join} from 'node:path';
import {sourceDataRoot} from '../../../../scripts/build/world/paths.mjs';
import {openProbeAgentSession,readProbeCharacterSpawnFromSession} from '../../../../scripts/lib/probeSession.mjs';
import {resetMissionMovementFixture} from '../../../../scripts/lib/missionMovementFixture.mjs';
import {assertCharacterAllowed} from '../../../../scripts/lib/probeCharacter.mjs';
import {bootPlayableSession} from './helpers/playable-session.mjs';

// Authenticated sight acquisition: only movement commands are sent. Never arm
// retaliation, inject a cast, mutate monster HP, or manufacture an aggro target.
test('Tomb Stone moving-target projectile capture',{timeout:180000},async()=>{
 const character=assertCharacterAllowed('asd2',{context:'live sight aggro'});
 const authority=await openProbeAgentSession();
 const original=await readProbeCharacterSpawnFromSession(authority,character);assert.ok(original);
 const table=await readFile(join(sourceDataRoot,'server','monster','population-evidence.tsv'),'utf8');
 const row=table.split('\n').map(line=>line.split('\t')).find(c=>c[0]==='MOB_CH_TOMBSTONE'&&c[1]==='24235'&&c[12]==='1');
 assert.ok(row,'authored aggressive field is required');
 const start={regionId:Number(row[1]),x:Number(row[2]),y:Number(row[3]),z:Number(row[4])};
 const out='temp/artifacts/tombstone-projectile/'+(process.env.SRO_AGGRO_CAPTURE??'current');await mkdir(out,{recursive:true});
 await writeFile(out+'/restore.json',JSON.stringify({character,original},null,2));
 let browser,page,failure;const errors=[];
 try {
  await resetMissionMovementFixture({session:authority,characterName:character,timeoutMs:30000,fixture:{id:'sight-aggro-authored-field',movementMode:3,start,startYawRadians:0}});
  ({browser,page}=await launchProbeBrowser());page.on('pageerror',error=>errors.push(error.message));
  await page.addInitScript(()=>{const Original=Worker;globalThis.__sightAggro={entities:{},samples:[],commands:[]};globalThis.Worker=class extends Original{constructor(...args){super(...args);this.addEventListener('message',({data})=>{if(data.kind!=='world'||!data.batch)return;for(const event of data.batch.events){if(event.kind==='state'||event.kind==='spawn')__sightAggro.entities[event.entity.gid]=event.entity;if(event.kind==='despawn')delete __sightAggro.entities[event.gid];}});}};});
  await page.route('**/src/engine/runtime/characters/effects/effects.ts',async route=>{const response=await route.fetch();let body=await response.text();body=body.replace('export function createCharacterEffects(', 'function createCharacterEffects(');body=body.replace(/if\s*\(impact\s*&&\s*impact.index\s*>=\s*0\)\s*impactEvents.push/, "if(flight&&cast.skill===173){globalThis.__sightAggro.launches??=[];globalThis.__sightAggro.launches.push({gid,source,destination,local:gameplay.pose});}if(impact&&impact.index>=0)impactEvents.push");body+=`
export {probeEffects as createCharacterEffects};
function probeEffects(...args){const owner=createCharacterEffects(...args),step=owner.step;owner.step=(...args)=>{const actors=step(...args);const g=args[1],r=globalThis.__sightAggro;if(r&&g&&g.casts.some(c=>c.skill===173)){r.effects??=[];if(r.effects.length<1200)r.effects.push({at:args[2],local:g.pose,entity:args[0].find(e=>e.gid===g.localGid),casts:g.casts.filter(c=>c.skill===173),actors:actors.map(a=>({gid:a.gid,model:a.model,pose:a.pose})),unsupported:owner.unsupported?.()});}return actors;};return owner;}`;await route.fulfill({response,body});});
  console.log('[sight-aggro] authenticated boot');await bootPlayableSession(page,character);
  await page.evaluate(()=>{const g=__playableRuntime.gameplay();if(__playableRuntime.entity(g.localGid)?.appearanceState?.[0]===2)__playableRuntime.session({kind:'gameplay',command:{kind:'rebirth',choice:2}});});
  await page.waitForFunction(()=>{const g=__playableRuntime.gameplay();return g.vitals.some(v=>v.gid===g.localGid&&v.hp>0);},null,{timeout:10000});
  console.log('[sight-aggro] approach without attack');
  await page.waitForFunction(()=>{
   const r=__sightAggro,g=__playableRuntime.gameplay(),now=performance.now();if(!g.pose)return false;
   r.samples.push({at:now,pose:g.pose,casts:g.casts,vitals:g.vitals});if(r.samples.length>500)r.samples.shift();
   const cast=g.casts.find(c=>c.caster!==g.localGid&&c.target===g.localGid&&r.entities[c.caster]?.kind==='monster');
   if(cast){r.firstMonsterCast=cast;r.monster=r.entities[cast.caster];return true;}
   if(r.lastMove&&now-r.lastMove<1200)return false;
   const candidates=Object.values(r.entities).filter(e=>e.kind==='monster'&&e.name==='Tomb Stone'&&e.regionId===g.pose.regionId&&e.appearanceState?.[0]!==2).sort((a,b)=>Math.hypot(a.x-g.pose.x,a.z-g.pose.z)-Math.hypot(b.x-g.pose.x,b.z-g.pose.z));
   const target=candidates[0];if(!target)return false;
   const destination={regionId:target.regionId,x:target.x,y:target.y,z:target.z,angle:0};
   const command={kind:'move',destination};r.commands.push({at:now,command,target:target.gid});r.lastMove=now;
   __playableRuntime.session({kind:'gameplay',command});return false;
  },null,{timeout:45000,polling:100});
  await page.evaluate(()=>{const p=__playableRuntime.gameplay().pose;__playableRuntime.session({kind:'gameplay',command:{kind:'move',destination:{...p,x:p.x+70}}});});
  await page.waitForTimeout(6500);
  const record=await page.evaluate(()=>__sightAggro);assert.ok(record.firstMonsterCast);assert.deepEqual(errors,[]);
  const launches=new Map((record.launches??[]).map(row=>[row.gid,row.local])),arrivals=new Set();
  for(const sample of record.effects??[])for(const actor of sample.actors){
   if(actor.model.includes('force_hit')&&!arrivals.has(actor.gid)){
    const launch=launches.get(actor.gid);assert.ok(launch,'arrival has an observed launch');
    assert.equal(actor.pose.regionId,launch.regionId);
    assert.ok(Math.abs(actor.pose.z-launch.z)<.001,'projectile captures live launch Z');
    assert.ok(Math.abs(Math.abs(actor.pose.x-launch.x)-2)<.001,'projectile captures live launch X plus authored offset');
    arrivals.add(actor.gid);
   }
  }
  assert.ok(arrivals.size>=2,'both authored Tomb Stone emitters reach their destinations');
 } catch(error) { failure=error;throw error; } finally {
  if(page){await writeFile(out+'/session.json',JSON.stringify({errors,failure:failure?.stack,row,original,record:await page.evaluate(()=>__sightAggro).catch(()=>null)},null,2));await page.screenshot({path:out+'/mission.png'}).catch(()=>{});await page.evaluate(()=>__playableRuntime?.session({kind:'logout'})).catch(()=>{});}
  await browser?.close();
  await resetMissionMovementFixture({session:authority,characterName:character,timeoutMs:30000,fixture:{id:'restore-sight-aggro-origin',movementMode:3,start:original,startYawRadians:original.angle/65535*Math.PI*2}});
 }
});
