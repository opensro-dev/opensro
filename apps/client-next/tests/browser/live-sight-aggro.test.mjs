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
test('live aggressive monster attacks a player who has not attacked it',{timeout:180000},async()=>{
 const character=assertCharacterAllowed('asd2',{context:'live sight aggro'});
 const authority=await openProbeAgentSession();
 const original=await readProbeCharacterSpawnFromSession(authority,character);assert.ok(original);
 const table=await readFile(join(sourceDataRoot,'server','monster','population-evidence.tsv'),'utf8');
 const row=table.split('\n').map(line=>line.split('\t')).find(c=>c[0]==='MOB_CH_TOMBSTONE'&&c[1]==='24235'&&c[12]==='1');
 assert.ok(row,'authored aggressive field is required');
 const start={regionId:Number(row[1]),x:Number(row[2]),y:Number(row[3]),z:Number(row[4])};
 const out='temp/artifacts/live-sight-aggro/'+(process.env.SRO_AGGRO_CAPTURE??'current');await mkdir(out,{recursive:true});
 await writeFile(out+'/restore.json',JSON.stringify({character,original},null,2));
 let browser,page,failure;const errors=[];
 try {
  await resetMissionMovementFixture({session:authority,characterName:character,timeoutMs:30000,fixture:{id:'sight-aggro-authored-field',movementMode:3,start,startYawRadians:0}});
  ({browser,page}=await launchProbeBrowser());page.on('pageerror',error=>errors.push(error.message));
  await page.addInitScript(()=>{const Original=Worker;globalThis.__sightAggro={entities:{},samples:[],commands:[]};globalThis.Worker=class extends Original{constructor(...args){super(...args);this.addEventListener('message',({data})=>{if(data.kind!=='world'||!data.batch)return;for(const event of data.batch.events){if(event.kind==='state'||event.kind==='spawn')__sightAggro.entities[event.entity.gid]=event.entity;if(event.kind==='despawn')delete __sightAggro.entities[event.gid];}});}};});
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
  const record=await page.evaluate(()=>__sightAggro);assert.ok(record.firstMonsterCast);assert.deepEqual(errors,[]);
 } catch(error) { failure=error;throw error; } finally {
  if(page){await writeFile(out+'/session.json',JSON.stringify({errors,failure:failure?.stack,row,original,record:await page.evaluate(()=>__sightAggro).catch(()=>null)},null,2));await page.screenshot({path:out+'/mission.png'}).catch(()=>{});await page.evaluate(()=>__playableRuntime?.session({kind:'logout'})).catch(()=>{});}
  await browser?.close();
  await resetMissionMovementFixture({session:authority,characterName:character,timeoutMs:30000,fixture:{id:'restore-sight-aggro-origin',movementMode:3,start:original,startYawRadians:original.angle/65535*Math.PI*2}});
 }
});
