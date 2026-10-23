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
import {installPursuitRecorder} from './helpers/pursuit-recorder.mjs';

// Authored population, authenticated packets and production movement only.
// The fixture changes offline placement; it does not manufacture hits or HP.
test('Logos Baroi retains a fleeing target through arrow release',{timeout:180000},async()=>{
 const character=assertCharacterAllowed('asd2',{context:'Baroi cast range regression'}),out='temp/artifacts/baroi-moving-target';
 const session=await openProbeAgentSession();assert.equal(session.divisionId,'test');
 const original=await readProbeCharacterSpawnFromSession(session,character);assert.ok(original);
 const rows=(await readFile(join(sourceDataRoot,'server','monster','population-supplement.tsv'),'utf8')).split('\n').map(r=>r.split('\t'));
 const row=rows.find(r=>r[0]==='MOB_EU_BAROI_CLON');assert.ok(row);
 const start={regionId:+row[1],x:+row[2],y:+row[3],z:+row[4]};
 await mkdir(out,{recursive:true});await writeFile(out+'/restore.json',JSON.stringify({character,original},null,2));
 const evidence={errors:[],start};let browser,page,tracing=false;
 try{
  await resetMissionMovementFixture({session,characterName:character,timeoutMs:30000,fixture:{id:'baroi-authored-field',movementMode:3,start,startYawRadians:0}});
  ({browser,page}=await launchProbeBrowser());page.on('pageerror',e=>evidence.errors.push(e.message));
  await installPursuitRecorder(page,[10,0x33a6]);
  await page.route('**/runtime/characters/effects/effects.ts*',async route=>{
   const response=await route.fetch(),source=await response.text();assert.ok(source.includes('export function createCharacterEffects('));
   const body=source.replace('export function createCharacterEffects(','function createObservedEffects(')+`
export function createCharacterEffects(...args){const owner=createObservedEffects(...args),step=owner.step;return {...owner,step(...args){const actors=step(...args),g=args[1];if(g?.casts.some(c=>c.skill===3610||c.skill===3611)){const r=globalThis.__baroiEffects??=[];if(r.length<2500)r.push({at:performance.now(),pose:g.pose,casts:g.casts,actors:actors.map(a=>({gid:a.gid,model:a.model,pose:a.pose}))});}return actors;}};}`;
   await route.fulfill({response,body});
  });
  console.log('[baroi] authenticated boot');await bootPlayableSession(page,character);
  await page.context().tracing.start({screenshots:true,snapshots:true});tracing=true;
  await page.evaluate(()=>{const g=__playableRuntime.gameplay();if(g.vitals.some(v=>v.gid===g.localGid&&v.hp===0))__playableRuntime.session({kind:'gameplay',command:{kind:'rebirth',choice:2}});});
  await page.waitForFunction(()=>{const g=__playableRuntime.gameplay();return g.vitals.some(v=>v.gid===g.localGid&&v.hp>0);},null,{timeout:10000});
  console.log('[baroi] acquire and flee during preparation');
  await page.waitForFunction(()=>{
   const r=__pursuit,g=__playableRuntime.gameplay(),now=performance.now();
   const cast=g.casts.find(c=>(c.skill===3610||c.skill===3611)&&c.target===g.localGid&&c.shotAtMs===undefined&&!c.results.length);
   if(cast){const m=r.entities[cast.caster];if(!m)return false;const dx=g.pose.x-m.x,dz=g.pose.z-m.z,d=Math.hypot(dx,dz)||1;
    if(d<100){if(!r.fleeing){r.fleeing=true;__playableRuntime.session({kind:'gameplay',command:{kind:'move',destination:{...g.pose,x:g.pose.x+dx/d*500,z:g.pose.z+dz/d*500}}});}return false;}
    r.baroi={cast,at:now,pose:g.pose,monster:m,vitals:g.vitals};
    __playableRuntime.session({kind:'gameplay',command:{kind:'move',destination:{...g.pose,x:g.pose.x+dx/d*300,z:g.pose.z+dz/d*300}}});return true;}
   if(r.lastApproach&&now-r.lastApproach<1200)return false;
   const m=Object.values(r.entities).filter(e=>e.kind==='monster'&&e.name==='Logos Baroi'&&e.regionId===g.pose.regionId&&e.appearanceState?.[0]!==2).sort((a,b)=>Math.hypot(a.x-g.pose.x,a.z-g.pose.z)-Math.hypot(b.x-g.pose.x,b.z-g.pose.z))[0];
   if(!m||r.provoked)return false;r.lastApproach=now;r.provoked=m.gid;__playableRuntime.session({kind:'gameplay',command:{kind:'attack',gid:m.gid}});return false;
  },null,{timeout:45000,polling:16});
  await page.waitForTimeout(2500);
  evidence.record=await page.evaluate(()=>({pursuit:__pursuit,effects:globalThis.__baroiEffects??[],game:__playableRuntime.gameplay()}));
  const r=evidence.record.pursuit,token=r.baroi.cast.token;
  const u32=(b,i)=>(b[i]|b[i+1]<<8|b[i+2]<<16|b[i+3]<<24)>>>0;
  const release=r.events.find(e=>e.kind==='wire'&&e.opcode===0xb505&&e.payload[0]===1&&u32(e.payload,1)===token);
  assert.ok(release,'owned Baroi cast releases real damage after the flee command');
  evidence.release=release;
  const before=r.baroi.vitals.find(v=>v.gid===evidence.record.game.localGid)?.hp,after=evidence.record.game.vitals.find(v=>v.gid===evidence.record.game.localGid)?.hp;
  assert.ok(after<before,'actual local HP decreases');
  const receipt=r.events.filter(e=>e.kind==='wire'&&e.opcode===10&&e.mainReceivedAt>=r.baroi.at&&e.mainReceivedAt<=release.mainReceivedAt).map(e=>({event:e,body:JSON.parse(new TextDecoder().decode(Uint8Array.from(e.payload)))})).find(e=>e.body.accepted&&e.body.world.moveSegment);
  assert.ok(receipt,'local movement accepted before release');evidence.movementReceipt=receipt;
  const atRelease=evidence.record.effects.findLast(s=>s.at<=release.mainReceivedAt);
  assert.ok(atRelease&&Math.hypot(atRelease.pose.x-r.baroi.pose.x,atRelease.pose.z-r.baroi.pose.z)>50,'player actually fled during wind-up');
  assert.ok(evidence.record.effects.some(s=>s.actors.length),'browser rendered combat effects');
  assert.deepEqual(evidence.errors,[]);evidence.verdict='PASS';
 }catch(error){evidence.failure=error.stack;throw error;}finally{
  if(page){evidence.record??=await page.evaluate(()=>({pursuit:__pursuit,effects:globalThis.__baroiEffects??[],game:__playableRuntime.gameplay()})).catch(()=>null);await page.screenshot({path:out+'/mission.png'}).catch(()=>{});if(tracing)await page.context().tracing.stop({path:out+'/trace.zip'}).catch(()=>{});await page.evaluate(()=>__playableRuntime?.session({kind:'logout'})).catch(()=>{});}
  await writeFile(out+'/incident.json',JSON.stringify(evidence,null,2));await browser?.close();
  await resetMissionMovementFixture({session,characterName:character,timeoutMs:30000,fixture:{id:'restore-baroi-origin',movementMode:3,start:original,startYawRadians:original.angle/65535*Math.PI*2}});
 }
});
