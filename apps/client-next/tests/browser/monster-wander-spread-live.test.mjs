import {test} from 'node:test';
import assert from 'node:assert/strict';
import {mkdir,readFile,writeFile} from 'node:fs/promises';
import {launchProbeBrowser} from '../../../../scripts/lib/probeBrowser.mjs';
import {join} from 'node:path';
import {sourceDataRoot} from '../../../../scripts/build/world/paths.mjs';
import {resetMissionMovementFixture} from '../../../../scripts/lib/missionMovementFixture.mjs';
import {openProbeAgentSession,readProbeCharacterSpawnFromSession} from '../../../../scripts/lib/probeSession.mjs';
import {assertCharacterAllowed} from '../../../../scripts/lib/probeCharacter.mjs';
import {bootPlayableSession} from './helpers/playable-session.mjs';

test('live ordinary Movia retain scattered positions while wandering',{timeout:180000},async()=>{
 const character=assertCharacterAllowed('asd2',{context:'monster wander spread regression'});
 const authority=await openProbeAgentSession(),original=await readProbeCharacterSpawnFromSession(authority,character);
 assert.ok(original);
 const table=await readFile(join(sourceDataRoot,'server','monster','population-supplement.tsv'),'utf8');
 const anchors=table.split(/\r?\n/).map(l=>l.split('\t')).filter(c=>c[0]==='MOB_EU_MOVOI').map(c=>({regionId:+c[1],x:+c[2],z:+c[4]}));
 const label=process.env.SRO_SPREAD_LABEL??'after',out='temp/artifacts/monster-wander-spread';await mkdir(out,{recursive:true});
 let browser,page;
 try{
  console.log('[wander-spread] position scratch character');
  await resetMissionMovementFixture({session:authority,characterName:character,timeoutMs:30000,fixture:{id:'movia-spread',movementMode:3,start:{regionId:27212,x:540,y:0,z:805},startYawRadians:0}});
  ({browser,page}=await launchProbeBrowser());
  await page.addInitScript(()=>{const Original=Worker;globalThis.__spread={};globalThis.Worker=class extends Original{constructor(...args){super(...args);this.addEventListener('message',({data})=>{if(data.kind==='world'&&data.batch)for(const e of data.batch.events){if(e.kind==='spawn'||e.kind==='state')__spread[e.entity.gid]=e.entity;else if(e.kind==='despawn')delete __spread[e.gid];}});}};});
  await bootPlayableSession(page,character);
  console.log('[wander-spread] sample 40 seconds of idle movement');
  const samples=[];
  for(let i=0;i<8;i++){
   await page.waitForTimeout(5000);
   samples.push(await page.evaluate(()=>Object.values(__spread).filter(e=>e.refObjId===5851&&(e.rarity??0)===0).map(e=>({gid:e.gid,regionId:e.regionId,x:e.x,z:e.z}))));
  }
  const rows=samples.at(-1).map(e=>({...e,nestDistance:Math.min(...anchors.filter(a=>a.regionId===e.regionId).map(a=>Math.hypot(e.x-a.x,e.z-a.z)))}));
  const outsideRing=rows.filter(e=>e.nestDistance>50).length;
  await writeFile(`${out}/${label}.json`,JSON.stringify({samples,rows,outsideRing},null,2));
  await page.screenshot({path:`${out}/${label}.png`});
  console.log('[wander-spread] '+JSON.stringify({count:rows.length,outsideRing}));
  assert.ok(rows.length>=5,'must observe ordinary Movia');
  assert.ok(outsideRing>rows.length/2,'most ordinary monsters must not collapse onto 30-unit nest rings');
 }finally{
  if(page)await page.evaluate(()=>__playableRuntime.session({kind:'logout'})).catch(()=>{});
  if(browser)await browser.close();
  await resetMissionMovementFixture({session:authority,characterName:character,timeoutMs:30000,fixture:{id:'restore-movia-spread',movementMode:3,start:original,startYawRadians:original.angle/65535*Math.PI*2}});
 }
});
