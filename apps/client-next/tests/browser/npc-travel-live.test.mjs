import {test} from 'node:test';
import assert from 'node:assert/strict';
import {mkdir,writeFile} from 'node:fs/promises';
import {launchProbeBrowser} from '../../../../scripts/lib/probeBrowser.mjs';
import {openProbeAgentSession,readProbeCharacterSpawnFromSession} from '../../../../scripts/lib/probeSession.mjs';
import {resetMissionMovementFixture} from '../../../../scripts/lib/missionMovementFixture.mjs';
import {assertCharacterAllowed} from '../../../../scripts/lib/probeCharacter.mjs';
import {bootPlayableSession} from './helpers/playable-session.mjs';
import {installPursuitRecorder} from './helpers/pursuit-recorder.mjs';

test('authored dock NPC renders and remains interactive after travelling',{timeout:240000},async()=>{
 const character=assertCharacterAllowed('asd2',{context:'dock NPC travel publication'});
 const session=await openProbeAgentSession(),original=await readProbeCharacterSpawnFromSession(session,character);
 assert.ok(original,'scratch restoration position');
 const out='temp/artifacts/npc-travel/'+(process.env.SRO_NPC_CAPTURE??'current');await mkdir(out,{recursive:true});
 await writeFile(out+'/restore.json',JSON.stringify({character,original},null,2));
 let browser,page,failure;const errors=[],stages=[];
 const capture=async name=>{
  const state=await page.evaluate(()=>({game:__playableRuntime.gameplay(),entities:__pursuit.entities,visible:globalThis.__pursuitVisible,events:__pursuit.events,ui:globalThis.__npcProbe?.owner.stats()}));
  console.log('[npc]',name,JSON.stringify(state.game.pose));
  stages.push({name,at:Date.now(),state});await page.screenshot({path:out+'/'+name+'.png'});await writeFile(out+'/stages.json',JSON.stringify(stages,null,2));return state;
 };
 try{
  await resetMissionMovementFixture({session,characterName:character,timeoutMs:30000,fixture:{id:'eastern-europe-dock',movementMode:3,start:{regionId:25163,x:1100,y:-155,z:280},startYawRadians:0}});
  ({browser,page}=await launchProbeBrowser());page.on('pageerror',e=>errors.push(e.message));
  await installPursuitRecorder(page,[0x30d7,0x36ab,0xb45a,0xb05a]);
  await page.route('**/src/engine/runtime/ui/ui.ts*',async route=>{
   const response=await route.fetch(),source=await response.text();assert.ok(source.includes('export function createUi('));
   await route.fulfill({response,body:source.replace('export function createUi(','function createObservedUi(')+'\nexport function createUi(ports){const owner=createObservedUi(ports);globalThis.__npcProbe={owner};return owner;}'});
  });
  await bootPlayableSession(page,character);await page.context().tracing.start({screenshots:true,snapshots:true});
  await page.waitForFunction(()=>Object.values(__pursuit.entities).some(e=>e.refObjId===7524),null,{timeout:15000});
  const dock=await capture('dock');console.log('[npc] dock',JSON.stringify({pose:dock.game.pose,npc:Object.values(dock.entities).find(e=>e.refObjId===7524),visible:dock.visible}));
  const dead=await page.evaluate(()=>{const g=__playableRuntime.gameplay();return g.vitals.some(v=>v.gid===g.localGid&&v.hp===0)});
  if(dead){await page.evaluate(()=>__playableRuntime.session({kind:'gameplay',command:{kind:'rebirth',choice:2}}));await page.waitForFunction(()=>{const g=__playableRuntime.gameplay();return g.vitals.some(v=>v.gid===g.localGid&&v.hp>0)},null,{timeout:10000});}
  const gid=Object.values(dock.entities).find(e=>e.refObjId===7524).gid;
  await page.mouse.move(350,200);await page.mouse.down({button:'right'});await page.mouse.move(350,320,{steps:12});await page.mouse.up({button:'right'});
  for(let i=0;i<12;i++)await page.mouse.wheel(0,120);
  const move=async(destination,name)=>{
   await page.evaluate(destination=>__playableRuntime.session({kind:'gameplay',command:{kind:'move',destination}}),destination);
   await page.waitForFunction(destination=>{const g=__playableRuntime.gameplay();return g.pose?.regionId===destination.regionId&&Math.hypot(g.pose.x-destination.x,g.pose.z-destination.z)<12&&!g.moving;},destination,{timeout:30000});await capture(name);
  };
  // Walk along the quay, not through the building immediately north of it.
  await move({regionId:25163,x:1300,y:-155,z:200,angle:0},'outside');
  await page.waitForFunction(gid=>!__pursuit.entities[gid],gid,{timeout:5000});
  await move({regionId:25163,x:1100,y:-155,z:280,angle:0},'returned');
  await page.waitForFunction(gid=>!!__pursuit.entities[gid],gid,{timeout:5000});
  const travel=await capture('travel-publication');
  const wireGid=e=>new DataView(Uint8Array.from(e.payload).buffer).getUint32(e.opcode===0x30d7?4:0,true);
  const wires=travel.events.filter(e=>e.kind==='wire'&&[0x30d7,0x36ab].includes(e.opcode)&&wireGid(e)===gid);
  assert.deepEqual(wires.map(e=>e.opcode),[0x36ab,0x30d7],'exactly one removal and recreation after bootstrap');
  await page.evaluate(gid=>__playableRuntime.session({kind:'gameplay',command:{kind:'select',gid}}),gid);
  await page.locator('[data-ui-id="npc-portal-open"]').waitFor({timeout:10000});await page.locator('[data-ui-id="npc-portal-open"]').click();
  await page.locator('[data-ui-id="npc-portal:31"]').waitFor({timeout:10000});await capture('ferry-menu');
  // Independent close-range visual acceptance on the manager's side of the
  // quay building. The travel phase above uses only admitted ground movement.
  await page.evaluate(()=>__playableRuntime.session({kind:'logout'}));
  await page.waitForFunction(()=>__playableRuntime.sessionState()?.phase==='signed-out');
  await resetMissionMovementFixture({session,characterName:character,timeoutMs:30000,fixture:{id:'dock-manager-visual',movementMode:3,start:{regionId:25163,x:958,y:-155,z:110},startYawRadians:0}});
  await bootPlayableSession(page,character);
  await page.waitForFunction(gid=>!!globalThis.__pursuitVisible?.anchors[gid],gid,{timeout:30000});
  await capture('manager-model');
  assert.deepEqual(errors,[]);
 }catch(error){failure=error; if(page)await capture('failure').catch(()=>{});}
 finally{
  if(page){await page.context().tracing.stop({path:out+'/trace.zip'}).catch(()=>{});await page.evaluate(()=>__playableRuntime?.session({kind:'logout'})).catch(()=>{});}
  await browser?.close();
  try{await resetMissionMovementFixture({session,characterName:character,timeoutMs:30000,fixture:{id:'restore-dock-probe',movementMode:3,start:original,startYawRadians:original.angle/65535*Math.PI*2}});}catch(error){failure??=error;}
  await writeFile(out+'/result.json',JSON.stringify({verdict:failure?'FAIL':'PASS',error:failure?.stack,errors},null,2));
 }
 if(failure)throw failure;
});
