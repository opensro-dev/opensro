import {test} from 'node:test';
import assert from 'node:assert/strict';
import {mkdir,writeFile} from 'node:fs/promises';
import {launchProbeBrowser} from '../../../../scripts/lib/probeBrowser.mjs';
import {openProbeAgentSession,readProbeCharacterSpawnFromSession} from '../../../../scripts/lib/probeSession.mjs';
import {resetMissionMovementFixture} from '../../../../scripts/lib/missionMovementFixture.mjs';
import {assertCharacterAllowed} from '../../../../scripts/lib/probeCharacter.mjs';
import {bootPlayableSession} from './helpers/playable-session.mjs';
import {installPursuitRecorder} from './helpers/pursuit-recorder.mjs';

// Authenticated production combat only: no HP mutation, fake death, or injected
// movement receipts. The offline fixture sets the scratch actor's starting area.
test('authenticated moving death and '+(process.env.SRO_REBIRTH_CAPTURE_MODE==='immediate'?'immediate resurrection animation':'delayed present rebirth'),{timeout:240000},async()=>{
 const immediate=process.env.SRO_REBIRTH_CAPTURE_MODE==='immediate';
 const character=assertCharacterAllowed('asd2',{context:'death movement regression'}),out=immediate?'temp/artifacts/death-movement/immediate':'temp/artifacts/death-movement/live';
 const session=await openProbeAgentSession();assert.equal(session.divisionId,'test','use the development Test shard');
 const original=await readProbeCharacterSpawnFromSession(session,character);assert.ok(original);
 await mkdir(out,{recursive:true});await writeFile(out+'/restore.json',JSON.stringify({character,original},null,2));
 const evidence={phases:[],commands:[],errors:[]};let browser,page,tracing=false;
 const capture=async(label)=>{const state=await page.evaluate(()=>{const g=__playableRuntime.gameplay();return {at:performance.now(),game:{pose:g.pose,localGid:g.localGid,vitals:g.vitals,rebirthPending:g.rebirthPending},entities:{[g.localGid]:__pursuit.entities[g.localGid]}};});evidence.phases.push({label,...state});return state;};
 const command=async(value)=>{evidence.commands.push({at:Date.now(),value});await page.evaluate(command=>__playableRuntime.session({kind:'gameplay',command}),value);};
 try{
  await resetMissionMovementFixture({session,characterName:character,timeoutMs:30000,fixture:{id:'death-movement-field',movementMode:3,start:{regionId:0x62a6,x:863,y:20,z:1746},startYawRadians:0}});
  ({browser,page}=await launchProbeBrowser());page.on('pageerror',e=>evidence.errors.push(e.message));await installPursuitRecorder(page,[10,0x33a6]);
  await page.route('**/runtime/characters/characters.ts*',async route=>{
   const response=await route.fetch(),source=await response.text();
   assert.ok(source.includes('export function createCharacterPresentation('));
   const body=source.replace('export function createCharacterPresentation(','function createObservedCharacters(')+`
export function createCharacterPresentation(...args){
 const renderer=args[1],health=args[6],record=globalThis.__rebirthAnimation={samples:[]};let game,entities;
 args[1]={...renderer,setCharacterActors(actors){const gid=game?.localGid,actor=actors.find(a=>a.gid===gid);if(actor&&record.samples.length<12000)record.samples.push({at:performance.now(),life:entities?.find(e=>e.gid===gid)?.appearanceState?.[0],healthDead:health?.dead(gid),pose:actor.pose,clip:actor.clip,time:actor.time,layers:actor.layers});return renderer.setCharacterActors(actors);}};
 const owner=createObservedCharacters(...args),step=owner.step;return {...owner,step(...values){entities=values[0];game=values[1];return step(...values);}};
}`;
   await route.fulfill({response,body});
  });
  console.log('[death-movement] authenticated boot');await bootPlayableSession(page,character);
  let initial=await capture('boot');
  if(initial.game.vitals.some(v=>v.gid===initial.game.localGid&&v.hp===0)){
   await command({kind:'rebirth',choice:2});await page.waitForFunction(()=>{const g=__playableRuntime.gameplay();return g.vitals.some(v=>v.gid===g.localGid&&v.hp>0);},null,{timeout:10000});
  }
  await page.context().tracing.start({screenshots:true,snapshots:true});tracing=true;
  await page.waitForFunction(()=>Object.values(__pursuit.entities).some(e=>e.kind==='monster'&&/mang|manyang/i.test(e.name??'')),null,{timeout:15000});
  const target=await page.evaluate(()=>{
   const g=__playableRuntime.gameplay(),p=g.pose;
   return Object.values(__pursuit.entities).filter(e=>e.kind==='monster'&&e.appearanceState?.[0]!==2&&e.regionId===p.regionId&&/mang|manyang/i.test(e.name??''))
    .sort((a,b)=>Math.hypot(a.x-p.x,a.z-p.z)-Math.hypot(b.x-p.x,b.z-p.z))[0];
  });assert.ok(target,'authored Mangyang must be resident');evidence.target=target;
  console.log('[death-movement] retaliating monster',target.gid);
  await command({kind:'attack',gid:target.gid});
  await page.waitForFunction(gid=>__pursuit.events.some(e=>e.kind==='wire'&&[0xb245,0xb505].includes(e.opcode)&&e.targetBefore?.gid===gid),target.gid,{timeout:20000});
  await command({kind:'move',destination:(await capture('engage')).game.pose});
  const deadline=Date.now()+130000;let dead;
  while(Date.now()<deadline){
   const state=await capture('combat');const g=state.game,local=state.entities[g.localGid];
   if(local?.appearanceState?.[0]===2){dead=state;break;}
   const hp=g.vitals.find(v=>v.gid===g.localGid)?.hp;
   if(hp!==undefined&&hp<35){
    // Alternate long destinations around the live pose at short intervals;
    // the actor stays in melee reach while each accepted path is unfinished.
    const sign=evidence.commands.length%2?1:-1;
    await command({kind:'move',destination:{...g.pose,x:g.pose.x+sign*100}});
   }
   await page.waitForTimeout(150);
  }
  assert.ok(dead,'production monster must kill the actor');console.log('[death-movement] death observed');
  const p=dead.game.pose;
  if(immediate){await page.locator('[data-ui-id="rebirth-alternate"]').click();}
  else {await page.screenshot({path:out+'/corpse.png'});await page.waitForTimeout(5000);const delayed=await capture('delayed-corpse');assert.deepEqual(delayed.game.pose,p,'corpse moved during dialog');await command({kind:'rebirth',choice:2});}
  await page.waitForFunction(()=>{const g=__playableRuntime.gameplay();return g.vitals.some(v=>v.gid===g.localGid&&v.hp>0)&&!g.rebirthPending;},null,{timeout:15000});
  const alive=await capture('revived');await page.screenshot({path:out+'/revived.png'});
  assert.equal(alive.game.pose.regionId,p.regionId);assert.ok(Math.hypot(alive.game.pose.x-p.x,alive.game.pose.z-p.z)<10,'rebirth jumped toward travel goal');
  if(immediate){
   evidence.revivedAt=await page.evaluate(()=>performance.now());
   await command({kind:'move',destination:{...alive.game.pose,x:alive.game.pose.x+300}});
   await page.waitForTimeout(2500);
   const moving=await capture('moving-after-revival');
   assert.equal(moving.entities[moving.game.localGid]?.appearanceState?.[0],1,'actor must still be alive');
   await page.screenshot({path:out+'/moving-after-revival.png'});
   const samples=await page.evaluate(()=>__rebirthAnimation.samples);
   const after=samples.filter(s=>s.at>evidence.revivedAt+500&&s.life===1);assert.ok(after.length>5);
   assert.ok(after.every(s=>!s.healthDead&&!/death|die/i.test(s.clip)&&!(s.layers??[]).some(l=>l.weight>0&&/death|die/i.test(l.clip))),'revived actor retained/reinstalled death presentation');
  }
  evidence.wire=await page.evaluate(()=>__pursuit.events.filter(e=>e.kind==='wire'));
  const death=evidence.wire.findLast(e=>e.opcode===0x3122&&e.payload[4]===0&&e.payload[5]===2&&new DataView(Uint8Array.from(e.payload).buffer).getUint32(0,true)===dead.game.localGid);
  const receipt=evidence.wire.filter(e=>e.opcode===10&&e.at<=death.at).map(e=>({event:e,body:JSON.parse(new TextDecoder().decode(Uint8Array.from(e.payload)))})).findLast(e=>e.body.accepted&&e.body.world.moveSegment);
  assert.ok(receipt,'must observe accepted in-flight movement before death');
  const segment=receipt.body.world.moveSegment;
  assert.ok(death.at-receipt.event.at<segment.arrivesAtMs-receipt.body.serverTimeMs,'death must occur before accepted travel finishes');
  assert.deepEqual(evidence.errors,[]);evidence.verdict='PASS';
 }catch(error){evidence.error=String(error);if(page){evidence.wire=await page.evaluate(()=>globalThis.__pursuit?.events??[]).catch(()=>[]);}throw error;}
 finally{
  if(tracing)await page.context().tracing.stop({path:out+'/trace.zip'});
  if(page){evidence.animation=await page.evaluate(()=>globalThis.__rebirthAnimation?.samples??[]).catch(()=>[]);await page.evaluate(()=>__playableRuntime.session({kind:'logout'})).catch(()=>{});}
  if(browser)await browser.close();
  await writeFile(out+'/incident.json',JSON.stringify(evidence,null,2));
  await resetMissionMovementFixture({session,characterName:character,timeoutMs:30000,fixture:{id:'death-movement-restore',movementMode:3,start:original,startYawRadians:original.angle/65535*Math.PI*2}});
 }
});
