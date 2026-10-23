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

// Promoted Movia (champion/giant) use champion tactics 189: aggressive, sight
// 150. Ordinary Movia keep passive tactics 190. Only movement commands are
// sent; the probe never attacks, casts, or manufactures an aggro target.
// 5851 MOB_EU_MOVOI ("Movoi"), 5850 MOB_EU_MOVOI_CLON ("Movia"). SRO_MOVIA_REF
// narrows the probe to one variant.
const MOVIA=new Set(process.env.SRO_MOVIA_REF?[Number(process.env.SRO_MOVIA_REF)]:[5850,5851]);
const MOVIA_CODES=new Set([...MOVIA].map(ref=>ref===5850?'MOB_EU_MOVOI_CLON':'MOB_EU_MOVOI'));
test('live promoted Movia acquires a player while ordinary Movia stay passive',{timeout:600000},async()=>{
 const character=assertCharacterAllowed('asd2',{context:'live champion aggro'});
 const authority=await openProbeAgentSession();
 const original=await readProbeCharacterSpawnFromSession(authority,character);assert.ok(original);
 const table=await readFile(join(sourceDataRoot,'server','monster','population-supplement.tsv'),'utf8');
 const anchors=table.split(/\r?\n/).map(line=>line.split('\t')).filter(c=>MOVIA_CODES.has(c[0])&&c[16]==='1'&&c[17]==='1')
  .map(c=>({code:c[0],regionId:Number(c[1]),x:Number(c[2]),y:Number(c[3]),z:Number(c[4])}));
 assert.ok(anchors.length>=10,'Movia champion tactics evidence is required');
 const out='temp/artifacts/live-champion-aggro';await mkdir(out,{recursive:true});
 await writeFile(out+'/restore.json',JSON.stringify({character,original},null,2));
 const attempts=[];let browser,page,failure,record=null;const errors=[];
 try {
  for(const anchor of anchors.slice(0,8)){
   const start={regionId:anchor.regionId,x:anchor.x,y:anchor.y,z:anchor.z};
   await resetMissionMovementFixture({session:authority,characterName:character,timeoutMs:30000,fixture:{id:'champion-aggro-movia-anchor',movementMode:3,start,startYawRadians:0}});
   ({browser,page}=await launchProbeBrowser());page.on('pageerror',error=>errors.push(error.message));
   await page.addInitScript(()=>{const Original=Worker;globalThis.__championAggro={entities:{},casts:[],commands:[]};globalThis.Worker=class extends Original{constructor(...args){super(...args);this.addEventListener('message',({data})=>{if(data.kind!=='world'||!data.batch)return;for(const event of data.batch.events){if(event.kind==='state'||event.kind==='spawn')__championAggro.entities[event.entity.gid]=event.entity;if(event.kind==='despawn')delete __championAggro.entities[event.gid];}});}};});
   await bootPlayableSession(page,character);
   await page.evaluate(()=>{const g=__playableRuntime.gameplay();if(__playableRuntime.entity(g.localGid)?.appearanceState?.[0]===2)__playableRuntime.session({kind:'gameplay',command:{kind:'rebirth',choice:2}});});
   await page.waitForFunction(()=>{const g=__playableRuntime.gameplay();return g.vitals.some(v=>v.gid===g.localGid&&v.hp>0);},null,{timeout:10000});
   // Allow the visibility plane to publish nearby monsters.
   await page.waitForTimeout(6000);
   const found=await page.waitForFunction(movia=>{
    const r=__championAggro,g=__playableRuntime.gameplay(),now=performance.now();if(!g.pose)return false;
    for(const cast of g.casts){const caster=r.entities[cast.caster];if(cast.target===g.localGid&&caster?.kind==='monster'&&!r.casts.some(c=>c.token===cast.token))r.casts.push({token:cast.token,caster:cast.caster,refObjId:caster.refObjId,rarity:caster.rarity,at:now});}
    const hit=r.casts.find(c=>movia.includes(c.refObjId));if(hit){r.first=hit;return true;}
    const promoted=Object.values(r.entities).filter(e=>e.kind==='monster'&&movia.includes(e.refObjId)&&(e.rarity===1||e.rarity===4)&&e.regionId===g.pose.regionId&&e.appearanceState?.[0]!==2)
     .sort((a,b)=>Math.hypot(a.x-g.pose.x,a.z-g.pose.z)-Math.hypot(b.x-g.pose.x,b.z-g.pose.z));
    if(!promoted.length){r.idleSince??=now;return now-r.idleSince>8000?'none':false;}
    r.target=promoted[0];r.targetSeenAt??=now;if(now-r.targetSeenAt>45000)return 'timeout';
    if(r.lastMove&&now-r.lastMove<1500)return false;
    const t=promoted[0],destination={regionId:t.regionId,x:t.x,y:t.y,z:t.z,angle:0};
    r.commands.push({at:now,target:t.gid,destination});r.lastMove=now;__playableRuntime.session({kind:'gameplay',command:{kind:'move',destination}});return false;
   },[...MOVIA],{timeout:70000,polling:100});
   const verdict=await found.jsonValue();record=await page.evaluate(()=>__championAggro);
   attempts.push({anchor,verdict,first:record.first,target:record.target&&{gid:record.target.gid,refObjId:record.target.refObjId,rarity:record.target.rarity},casts:record.casts});
   await page.screenshot({path:out+'/anchor-'+attempts.length+'.png'}).catch(()=>{});
   await page.evaluate(()=>__playableRuntime.session({kind:'logout'})).catch(()=>{});await browser.close();browser=undefined;page=undefined;
   if(verdict===true)break;
  }
  const success=attempts.find(a=>a.verdict===true);
  assert.ok(success,'no promoted Movia acquired the player: '+JSON.stringify(attempts.map(a=>({anchor:a.anchor,verdict:a.verdict}))));
  assert.ok(success.first.rarity===1||success.first.rarity===4,'first Movia attacker must be a champion or giant');
  const ordinary=attempts.flatMap(a=>a.casts).filter(c=>MOVIA.has(c.refObjId)&&(c.rarity??0)===0);
  assert.deepEqual(ordinary,[],'ordinary Movia must not acquire an unattacked player');
  assert.deepEqual(errors,[]);
 } catch(error) { failure=error;throw error; } finally {
  await writeFile(out+'/session.json',JSON.stringify({errors,failure:failure?.stack,original,attempts},null,2));
  if(page)await page.evaluate(()=>__playableRuntime?.session({kind:'logout'})).catch(()=>{});
  await browser?.close();
  await resetMissionMovementFixture({session:authority,characterName:character,timeoutMs:30000,fixture:{id:'restore-champion-aggro-origin',movementMode:3,start:original,startYawRadians:original.angle/65535*Math.PI*2}});
 }
});
