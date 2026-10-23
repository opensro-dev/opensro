import {test} from 'node:test';
import assert from 'node:assert/strict';
import {mkdir,writeFile,readFile} from 'node:fs/promises';
import {launchProbeBrowser} from '../../../../scripts/lib/probeBrowser.mjs';
import {join} from 'node:path';
import {sourceDataRoot} from '../../../../scripts/build/world/paths.mjs';
import {bootPlayableSession} from './helpers/playable-session.mjs';
import {openProbeAgentSession,readProbeCharacterSpawnFromSession} from '../../../../scripts/lib/probeSession.mjs';
import {resetMissionMovementFixture} from '../../../../scripts/lib/missionMovementFixture.mjs';
import {assertCharacterAllowed} from '../../../../scripts/lib/probeCharacter.mjs';
import {holdProbeRuntime} from './helpers/hold-runtime.mjs';

test(process.env.SRO_SELECTION_DECAL==='1'?'same-monster double-click restores the shared decal after ground movement':process.env.SRO_SKILL_SLASH==='1'?'learned Slash activates from Digit1 and continues with basic attack':'learned weapon imbue activates and offensive skill continues with basic attack',{timeout:180000},async()=>{
 const slashMode=process.env.SRO_SKILL_SLASH==='1',slot=slashMode?1:7;
 const character=process.env.SRO_PROBE_CHARACTER??'asd3',captureOnly=process.env.SRO_SKILL_CAPTURE_ONLY==='1';
 const waterGhost=process.env.SRO_SKILL_WATER_GHOST==='1',targetName=waterGhost?'Water Ghost':'Tomb Stone';
 const directory=(process.env.SRO_SELECTION_DECAL==='1'?'temp/artifacts/selection-decal/':slashMode?'temp/artifacts/slash-hotkey/':'temp/artifacts/skill-activation/')+(captureOnly?'before':'after');await mkdir(directory,{recursive:true});
 const checked=assertCharacterAllowed(character,{context:'skill activation'}),authority=await openProbeAgentSession();
 const original=await readProbeCharacterSpawnFromSession(authority,checked);assert.ok(original);
 const table=await readFile(join(sourceDataRoot,'server','monster','population-evidence.tsv'),'utf8');
 const anchor=table.split('\n').map(l=>l.split('\t')).find(c=>waterGhost?c[0]==='MOB_CH_WATERGHOST'&&c[1]==='25511':c[0]==='MOB_CH_TOMBSTONE'&&c[1]==='24235'&&c[12]==='1');assert.ok(anchor);
 const start={regionId:Number(anchor[1]),x:Number(anchor[2]),y:Number(anchor[3]),z:Number(anchor[4])};
 const {browser,page}=await launchProbeBrowser(),report={character,wire:[],errors:[],samples:[]};let originalBinding,restored=false,weaponSwap,ammoSwap,originalBodyStatus;
 const revive=async()=>{
  const dead=await page.evaluate(()=>{const g=__playableRuntime.gameplay();return g.vitals.some(v=>v.gid===g.localGid&&v.hp===0);});
  if(dead){await page.evaluate(()=>__playableRuntime.session({kind:'gameplay',command:{kind:'rebirth',choice:2}}));await page.waitForFunction(()=>{const g=__playableRuntime.gameplay();return g.vitals.some(v=>v.gid===g.localGid&&v.hp>0);},null,{timeout:10000});}
  return dead;
 };
 page.on('pageerror',e=>report.errors.push(String(e)));
 page.on('console',m=>{if(m.text().startsWith('[skill-wire]'))report.wire.push(JSON.parse(m.text().slice(12)));});
 try{
  await writeFile(directory+'/restore.json',JSON.stringify({character,original},null,2));
  await resetMissionMovementFixture({session:authority,characterName:checked,timeoutMs:30000,fixture:{id:'skill-activation-authored-target',movementMode:3,start,startYawRadians:0}});
  await holdProbeRuntime(page);
  await page.route('**/runtime/simulation/worker/network/network.ts*',async route=>{
   const response=await route.fetch();let body=await response.text();
   const inbound='const frame = codec.decode(new Uint8Array(event.data));',outbound='socket.send(encoded);';assert.ok(body.includes(inbound)&&body.includes(outbound));
   const observe=direction=>`if(frame.opcode!==1&&frame.opcode!==6)console.log('[skill-wire]'+JSON.stringify({direction:'${direction}',at:performance.now(),opcode:frame.opcode,payload:Array.from(frame.payload)}));`;
   await route.fulfill({response,body:body.replace(inbound,inbound+observe('in')).replace(outbound,outbound+observe('out'))});
  });
  await page.route('**/runtime/renderer/renderer.ts*',async route=>{
   const response=await route.fetch(),source=await response.text();assert.ok(source.includes('export function createRenderer('));
   await route.fulfill({response,body:source.replace('export function createRenderer(','function observedRenderer(')+`\nexport function createRenderer(...args){const owner=observedRenderer(...args);globalThis.__skillRenderer=owner;return {...owner,setCharacterActors(rows){globalThis.__skillActors=rows;return owner.setCharacterActors(rows)}};}`});
  });
  console.log('[skill] boot '+character);await bootPlayableSession(page,character);
  if(await revive()){
   report.revivedBefore=true;await page.evaluate(()=>__playableRuntime.session({kind:'logout'}));await page.waitForFunction(()=>__playableRuntime.sessionState()?.phase==='signed-out');
   await resetMissionMovementFixture({session:authority,characterName:checked,timeoutMs:30000,fixture:{id:'skill-activation-after-rebirth',movementMode:3,start,startYawRadians:0}});
   await bootPlayableSession(page,character);
  }
  await page.waitForFunction(()=>__playableRuntime.gameplay().skillCatalog?.length&&globalThis.__skillActors?.length);
  report.eligibility=await page.evaluate(()=>__playableRuntime.gameplay().eligibility);
  originalBodyStatus=await page.evaluate(()=>__playableRuntime.entity(__playableRuntime.gameplay().localGid)?.appearanceState?.[2]??0);
  assert.ok([0,3].includes(originalBodyStatus),'probe requires ordinary or already invincible body status');
  if(report.eligibility.gm&&originalBodyStatus===0){await page.evaluate(()=>__playableRuntime.session({kind:'gameplay',command:{kind:'gm-command',line:'/INVINCIBLE'}}));await page.waitForFunction(()=>__playableRuntime.entity(__playableRuntime.gameplay().localGid)?.appearanceState?.[2]===3,null,{timeout:5000});}
  report.invincibleDuringProbe=report.eligibility.gm||originalBodyStatus===3;
  await page.waitForFunction(()=>__playableRuntime.gameplay().skillCatalog?.length&&globalThis.__skillActors?.length);
  report.initial=await page.evaluate(()=>{const g=__playableRuntime.gameplay();return {gid:g.localGid,skills:g.skills,catalog:g.skillCatalog.filter(r=>g.skills.includes(r.id)),inventory:g.inventory,quickSlots:g.quickSlots,weapon:g.inventory.find(r=>r.slot===6),pose:g.pose,vitals:g.vitals};});
  const fire=report.initial.catalog.find(r=>r.nameSymbol==='SN_SKILL_CH_FIRE_GIGONGTA_A');const strikeId=slashMode?report.initial.catalog.find(r=>r.nameSymbol==='SN_SKILL_EU_WARRIOR_ONEHANDA_STRIKE_A')?.id:3;assert.ok(strikeId,'learned Slash required');if(!slashMode){assert.ok(fire,'learned River fire force required');assert.ok(report.initial.skills.includes(3),'learned Strike n smash required');}
  originalBinding=report.initial.quickSlots.find(r=>r.slot===slot);
  const weaponKinds=slashMode?[7]:[2,3];
  if(!weaponKinds.includes(report.initial.weapon.typeFlags>>>11)){
   const blade=report.initial.inventory.find(r=>r.slot>=13&&weaponKinds.includes(r.typeFlags>>>11));assert.ok(blade,'owned sword/blade required for Strike n smash');
   const move=async(source,destination,ref)=>{await page.evaluate(({source,destination})=>__playableRuntime.session({kind:'gameplay',command:{kind:'inventory-move',source,destination,quantity:1}}),{source,destination});await page.waitForFunction(({destination,ref})=>__playableRuntime.gameplay().inventory.some(r=>r.slot===destination&&r.refObjId===ref),{destination,ref},{timeout:5000});};
   const offhand=report.initial.inventory.find(r=>r.slot===7);
   if(offhand){const empty=await page.evaluate(()=>{const g=__playableRuntime.gameplay();for(let i=13;i<g.inventorySlotCount;i++)if(!g.inventory.some(r=>r.slot===i))return i;return -1;});assert.ok(empty>=13,'bag slot required to preserve arrows');ammoSwap={slot:empty,ref:offhand.refObjId,quantity:offhand.quantity};await page.evaluate(({destination,quantity})=>__playableRuntime.session({kind:'gameplay',command:{kind:'inventory-move',source:7,destination,quantity}}),{destination:empty,quantity:offhand.quantity});await page.waitForFunction(({slot,ref})=>__playableRuntime.gameplay().inventory.some(r=>r.slot===slot&&r.refObjId===ref),ammoSwap,{timeout:5000});}
   weaponSwap={slot:blade.slot,ref:report.initial.weapon.refObjId};await move(blade.slot,6,blade.refObjId);
  }
  const bind=async payload=>{await page.evaluate(({payload,slot})=>__playableRuntime.session({kind:'gameplay',command:{kind:'quickslot-set',binding:{slot,kind:0x49,payload}}}),{payload,slot});await page.waitForFunction(({payload,slot})=>__playableRuntime.gameplay().quickSlots.some(r=>r.slot===slot&&r.payload===payload),{payload,slot});};
  const sample=()=>page.evaluate(()=>{const g=__playableRuntime.gameplay();return {at:performance.now(),casts:g.casts,effects:g.attachedEffects,cooldowns:g.skillCooldowns,error:g.error,vitals:g.vitals,actors:__skillActors.filter(a=>a.gid===g.localGid||a.gid<0).map(a=>({gid:a.gid,model:a.model,clip:a.clip,layers:a.layers}))};});
  if(!slashMode){console.log('[skill] activate River fire force');await bind(fire.id);await page.keyboard.press(slashMode?'Digit1':'Digit7');
  for(let i=0;i<15;i++){report.samples.push(await sample());await page.waitForTimeout(100);}
  report.imbueActivated=report.samples.some(s=>s.effects?.some(e=>e.gid===report.initial.gid&&e.skill===fire.id&&[2,3].includes(e.phase)));
  await page.screenshot({path:directory+'/imbue.png'});
  }
  await page.waitForTimeout(3600);
  await bind(strikeId);
  let target;
  for(let turn=0;turn<8&&!target;turn++){
   target=await page.evaluate(targetName=>{const local=__playableRuntime.gameplay().localGid;for(let y=.15;y<.7;y+=.025)for(let x=.04;x<.96;x+=.025){if(document.elementFromPoint(x*innerWidth,y*innerHeight)?.closest('[data-ui-id]'))continue;const gid=__skillRenderer.pickEntity(x,y,local),e=gid&&__playableRuntime.entity(gid);if(e?.kind==='monster'&&e.name===targetName&&e.appearanceState?.[0]!==2&&__skillActors.some(a=>a.gid===gid))return {gid,x,y,name:e.name};}return null;},targetName);
   if(!target){await page.mouse.move(500,350);await page.mouse.down({button:'right'});await page.mouse.move(660,350,{steps:8});await page.mouse.up({button:'right'});await page.waitForTimeout(150);}
  }
  assert.ok(target,'visible living target required');report.target=target;console.log('[skill] '+(slashMode?'Slash':'Strike n smash')+' on '+target.name);
  const viewport=page.viewportSize();await page.mouse.click(target.x*viewport.width,target.y*viewport.height);
  await page.waitForFunction(gid=>__playableRuntime.gameplay().target===gid,target.gid);
  if(process.env.SRO_SELECTION_DECAL==='1'){
   report.decalCycles=[];
   for(let cycle=0;cycle<3;cycle++){
    const ground=await page.evaluate(()=>{const g=__playableRuntime.gameplay();for(let y=.55;y<.7;y+=.025)for(let x=.3;x<.7;x+=.025){if(document.elementFromPoint(x*innerWidth,y*innerHeight)?.closest('[data-ui-id]'))continue;if(__skillRenderer.pickEntity(x,y,g.localGid)!==null)continue;if(__skillRenderer.pickGround(x,y))return {x,y};}return null;});assert.ok(ground,'visible ground');
    await page.mouse.click(ground.x*viewport.width,ground.y*viewport.height);
    await page.waitForFunction(()=>__playableRuntime.gameplay().selectionDecal?.kind==='ground',null,{timeout:3000});
    await page.screenshot({path:directory+'/decal-ground-'+cycle+'.png'});
    await page.waitForFunction(()=>!__playableRuntime.gameplay().moving&&__playableRuntime.gameplay().pendingMoves===0,null,{timeout:20000});
    const point=await page.evaluate(gid=>{const g=__playableRuntime.gameplay(),hits=[];for(let y=.15;y<.7;y+=.012)for(let x=.04;x<.96;x+=.012){if(document.elementFromPoint(x*innerWidth,y*innerHeight)?.tagName!=='CANVAS')continue;if(__skillRenderer.pickEntity(x,y,g.localGid)===gid)hits.push({x,y});}if(!hits.length)return null;const center=hits.reduce((a,p)=>({x:a.x+p.x/hits.length,y:a.y+p.y/hits.length}),{x:0,y:0});return hits.sort((a,b)=>Math.hypot(a.x-center.x,a.y-center.y)-Math.hypot(b.x-center.x,b.y-center.y))[0];},target.gid);assert.ok(point,'same living monster remains visible');
    await page.mouse.dblclick(point.x*viewport.width,point.y*viewport.height);
    await page.waitForFunction(gid=>{const d=__playableRuntime.gameplay().selectionDecal;return d?.kind==='target'&&d.gid===gid;},target.gid,{timeout:3000});
    await page.screenshot({path:directory+'/decal-target-'+cycle+'.png'});
    report.decalCycles.push(await page.evaluate(()=>{const g=__playableRuntime.gameplay();return {target:g.target,decal:g.selectionDecal};}));
    await page.evaluate(()=>__playableRuntime.session({kind:'gameplay',command:{kind:'cancel'}}));
   }
   assert.deepEqual(report.errors,[]);return;
  }
  report.mpBeforeHotkey=await page.evaluate(()=>{const g=__playableRuntime.gameplay();return g.vitals.find(v=>v.gid===g.localGid)?.mp;});
  await page.keyboard.press(slashMode?'Digit1':'Digit7');
  for(let i=0;i<100;i++){
   const row=await sample();report.samples.push(row);
   const own=row.casts.filter(c=>c.caster===report.initial.gid&&c.target===target.gid),strike=own.find(c=>c.skill===strikeId);
   if(!slashMode&&strike&&!report.concurrentRequest){report.concurrentRequest=true;report.concurrentAt=row.at;report.attackOpenAtImbueRequest=strike.cancelledAtMs===undefined;await bind(fire.id);await page.keyboard.press(slashMode?'Digit1':'Digit7');}
   if(strike&&own.some(c=>c.skill!==strikeId&&c.receivedAtMs>strike.receivedAtMs))break;
   await page.waitForTimeout(100);
  }
  await page.evaluate(()=>__playableRuntime.session({kind:'gameplay',command:{kind:'cancel'}}));
  const casts=report.samples.flatMap(s=>s.casts??[]).filter(c=>c.caster===report.initial.gid&&c.target===target.gid),strike=casts.find(c=>c.skill===strikeId);
  report.strike=strike;report.continued=!!strike&&casts.some(c=>c.skill!==strikeId&&c.receivedAtMs>strike.receivedAtMs);
  if(slashMode&&strike){const committed=report.samples.find(s=>s.casts.some(c=>c.token===strike.token));report.mpAfterSlash=committed?.vitals.find(v=>v.gid===report.initial.gid)?.mp;if(!captureOnly)assert.equal(report.mpBeforeHotkey-report.mpAfterSlash,11,'level-one Slash must debit MP exactly once');}
  report.concurrentImbueActivated=report.samples.some(s=>s.at>report.concurrentAt&&s.effects?.some(e=>e.gid===report.initial.gid&&e.skill===fire.id&&[2,3].includes(e.phase)));
  await page.screenshot({path:directory+'/combat.png'});
  if(!slashMode&&report.invincibleDuringProbe){
  // Reapply in melee range so the five-second imbue cannot expire during pursuit.
  await page.waitForTimeout(1600);await bind(fire.id);await page.keyboard.press(slashMode?'Digit1':'Digit7');await page.waitForTimeout(300);
  await page.evaluate(gid=>__playableRuntime.session({kind:'gameplay',command:{kind:'attack',gid}}),target.gid);
  for(let i=0;i<70;i++){const row=await sample();report.samples.push(row);if(!report.burnVisible&&row.actors.some(a=>a.model.toLowerCase().includes('status_bad_burn.efp'))){report.burnVisible=true;await page.screenshot({path:directory+'/burn.png'});}await page.waitForTimeout(100);}
  report.burnDamagePublished=report.wire.some(f=>f.direction==='in'&&f.opcode===0x3128);
  }
  if(!captureOnly){if(!slashMode)assert.ok(report.imbueActivated,'fire imbue must be admitted');assert.ok(strike,'Strike n smash must be accepted');if(!slashMode)assert.ok(report.concurrentImbueActivated&&report.attackOpenAtImbueRequest,'instant imbue must activate during an open attack');assert.ok(report.continued,'Strike n smash must transfer to basic attack against the surviving authored target');assert.deepEqual(report.errors,[]);}
 }catch(error){report.failure=String(error);report.failureState=await page.evaluate(()=>({gameplay:__playableRuntime.gameplay(),entity:__playableRuntime.entity(__playableRuntime.gameplay().localGid)})).catch(()=>null);throw error;}finally{
  try{if(originalBinding!==undefined||report.initial){const binding=originalBinding??{slot,kind:0,payload:0};await page.evaluate(binding=>__playableRuntime.session({kind:'gameplay',command:{kind:'quickslot-set',binding}}),binding);await page.waitForFunction(binding=>__playableRuntime.gameplay().quickSlots.some(r=>r.slot===binding.slot&&r.kind===binding.kind&&r.payload===binding.payload)||binding.kind===0&&!__playableRuntime.gameplay().quickSlots.some(r=>r.slot===binding.slot),binding,{timeout:5000});restored=true;}}catch{}
  try{
   await page.evaluate(()=>__playableRuntime?.session({kind:'gameplay',command:{kind:'cancel'}}));
   report.revivedAfter=await revive();
   if(weaponSwap){await page.evaluate(({slot})=>__playableRuntime.session({kind:'gameplay',command:{kind:'inventory-move',source:slot,destination:6,quantity:1}}),weaponSwap);await page.waitForFunction(({ref})=>__playableRuntime.gameplay().inventory.some(r=>r.slot===6&&r.refObjId===ref),weaponSwap,{timeout:5000});}
   if(ammoSwap){await page.evaluate(({slot,quantity})=>__playableRuntime.session({kind:'gameplay',command:{kind:'inventory-move',source:slot,destination:7,quantity}}),ammoSwap);await page.waitForFunction(({ref,quantity})=>__playableRuntime.gameplay().inventory.some(r=>r.slot===7&&r.refObjId===ref&&r.quantity===quantity),ammoSwap,{timeout:5000});}
   report.equipmentRestored=true;
  }catch(error){report.equipmentRestoreError=String(error);}
  try{
   if(originalBodyStatus===0&&await page.evaluate(()=>__playableRuntime.entity(__playableRuntime.gameplay().localGid)?.appearanceState?.[2]===3)){await page.evaluate(()=>__playableRuntime.session({kind:'gameplay',command:{kind:'gm-command',line:'/INVINCIBLE'}}));await page.waitForFunction(()=>__playableRuntime.entity(__playableRuntime.gameplay().localGid)?.appearanceState?.[2]===0,null,{timeout:5000});}
   report.finalVitals=await page.evaluate(()=>{const g=__playableRuntime.gameplay();return g.vitals.find(v=>v.gid===g.localGid);});report.bodyStatusRestored=true;
  }catch(error){report.bodyStatusRestoreError=String(error);}
  report.bindingRestored=restored;await writeFile(directory+'/incident.json',JSON.stringify(report,null,2));await page.evaluate(()=>__playableRuntime?.session({kind:'logout'})).catch(()=>{});await browser.close();
  await resetMissionMovementFixture({session:authority,characterName:checked,timeoutMs:30000,fixture:{id:'restore-skill-activation-origin',movementMode:3,start:original,startYawRadians:original.angle/65535*Math.PI*2}});
  if(!report.failure)assert.ok(report.bindingRestored&&report.equipmentRestored&&report.bodyStatusRestored&&report.finalVitals?.hp>0,'probe must restore a living character, original body status, equipment and binding');
 }
});
