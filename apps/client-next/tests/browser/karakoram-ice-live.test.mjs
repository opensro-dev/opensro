import {test} from 'node:test';
import assert from 'node:assert/strict';
import {mkdir,writeFile} from 'node:fs/promises';
import {launchProbeBrowser} from '../../../../scripts/lib/probeBrowser.mjs';
import {openProbeAgentSession,readProbeCharacterSpawnFromSession} from '../../../../scripts/lib/probeSession.mjs';
import {resetMissionMovementFixture} from '../../../../scripts/lib/missionMovementFixture.mjs';
import {assertCharacterAllowed} from '../../../../scripts/lib/probeCharacter.mjs';
import {bootPlayableSession} from './helpers/playable-session.mjs';
import {holdProbeRuntime} from './helpers/hold-runtime.mjs';
import {installPursuitRecorder} from './helpers/pursuit-recorder.mjs';

test('Karakoram ice supports the player and resident Penons',{timeout:150000,skip:!process.env.SRO_ICE_CAPTURE},async()=>{
 const baseline=process.env.SRO_ICE_CAPTURE==='baseline',character=assertCharacterAllowed('asd2',{context:'Karakoram ice regression'});
 const out='temp/artifacts/karakoram-ice/'+(baseline?'before':'after'),evidence={character,baseline,phases:[],errors:[]};
 const session=await openProbeAgentSession(),original=await readProbeCharacterSpawnFromSession(session,character);assert.ok(original);evidence.shard=session.divisionId;
 await mkdir(out,{recursive:true});let browser,page,tracing=false;
 const capture=async label=>{const state=await page.evaluate(()=>{const g=__playableRuntime.gameplay();return {pose:g.pose,local:__playableRuntime.entity(g.localGid),hp:g.vitals.find(v=>v.gid===g.localGid)?.hp,monsters:Object.values(__pursuit.entities).filter(e=>e.kind==='monster').map(e=>({gid:e.gid,name:e.name,refObjId:e.refObjId,regionId:e.regionId,x:e.x,y:e.y,z:e.z}))};});evidence.phases.push({label,...state});return state;};
 try{
  await resetMissionMovementFixture({session,characterName:character,timeoutMs:30000,fixture:{id:'karakoram-ice',movementMode:3,start:{regionId:0x5c81,x:750,y:690,z:410},startYawRadians:0}});
  ({browser,page}=await launchProbeBrowser());await holdProbeRuntime(page);await installPursuitRecorder(page,[10]);page.on('pageerror',e=>evidence.errors.push(e.message));
  console.log('[ice] authenticated boot');await bootPlayableSession(page,character);
  await page.context().tracing.start({screenshots:true,snapshots:true});tracing=true;
  await page.waitForFunction(()=>Object.values(__pursuit.entities).some(e=>e.kind==='monster'),null,{timeout:15000});
  const initial=await capture('at-reported-coordinate');await page.screenshot({path:out+'/ice.png'});
  const lakeMonsters=initial.monsters.filter(e=>e.regionId===0x5c81&&e.x<1000&&e.z<600);
  if(baseline){assert.ok(initial.pose.y<750);}
  else{
   assert.equal(initial.pose.y,800);assert.ok(lakeMonsters.length>0,'capture must include resident lake monsters');assert.ok(lakeMonsters.every(e=>e.y>=800),'resident lake monsters must use the ice surface');
   if(initial.hp===0){await page.evaluate(()=>__playableRuntime.session({kind:'gameplay',command:{kind:'rebirth',choice:2}}));await page.waitForFunction(()=>{const g=__playableRuntime.gameplay();return g.vitals.find(v=>v.gid===g.localGid)?.hp>0;},null,{timeout:10000});}
   await page.evaluate(()=>{const g=__playableRuntime.gameplay();__playableRuntime.session({kind:'gameplay',command:{kind:'move',destination:{...g.pose,x:800,z:430}}});});
   await page.waitForFunction(()=>__playableRuntime.gameplay().pose.x>780,null,{timeout:10000});
   const moved=await capture('moved-on-ice');assert.equal(moved.pose.y,800);await page.screenshot({path:out+'/moved.png'});
  }
  evidence.wire=await page.evaluate(()=>__pursuit.events.filter(e=>e.kind==='wire'));assert.deepEqual(evidence.errors,[]);evidence.verdict=baseline?'BASELINE REPRODUCED':'PASS SUCCESS';
 }catch(error){evidence.error=String(error);if(page){await capture('failure').catch(()=>{});}throw error;}
 finally{
  if(tracing)await page.context().tracing.stop({path:out+'/trace.zip'});
  if(page)await page.evaluate(()=>__playableRuntime.session({kind:'logout'})).catch(()=>{});if(browser)await browser.close();
  await writeFile(out+'/incident.json',JSON.stringify(evidence,null,2));
  await resetMissionMovementFixture({session,characterName:character,timeoutMs:30000,fixture:{id:'karakoram-ice-restore',movementMode:3,start:original,startYawRadians:original.angle/65535*Math.PI*2}});
 }
});
