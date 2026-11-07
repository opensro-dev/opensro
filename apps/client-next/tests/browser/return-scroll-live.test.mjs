import {test} from 'node:test';
import assert from 'node:assert/strict';
import {mkdir,writeFile} from 'node:fs/promises';
import {launchProbeBrowser} from '../../../../scripts/lib/probeBrowser.mjs';
import {bootPlayableSession} from './helpers/playable-session.mjs';
import {resetMissionMovementFixture} from '../../../../scripts/lib/missionMovementFixture.mjs';
import {openProbeAgentSession,readProbeCharacterSpawnFromSession} from '../../../../scripts/lib/probeSession.mjs';

test('return scroll uses real double-click, server cancellation and timed world re-entry',{timeout:180000},async()=>{
 const authority=await openProbeAgentSession(),original=await readProbeCharacterSpawnFromSession(authority,'asd3');
 const out='temp/artifacts/inventory-return/live-after-'+Date.now();await mkdir(out,{recursive:true});
 const {browser,page}=await launchProbeBrowser(),errors=[],stages=[],wire=[];
 page.on('websocket',socket=>socket.on('framereceived',f=>{if(Buffer.isBuffer(f.payload)&&f.payload.length>=2&&wire.length<200){const opcode=f.payload.readUInt16LE(0);if([0x3369,0x3449,0xb5bd,0xb2dd,0x3122,0xb2f5].includes(opcode))wire.push({opcode,length:f.payload.length});}}));
 page.on('pageerror',e=>errors.push(e.message));
 try{
  await resetMissionMovementFixture({session:authority,characterName:'asd3',timeoutMs:30000,fixture:{id:'return-scroll-accessory-v1',movementMode:3,start:{regionId:25000,x:1600,y:0,z:1078},startYawRadians:0}});
  await page.route('**/src/engine/runtime/ui/ui.ts',async route=>{const response=await route.fetch(),source=await response.text();await route.fulfill({response,body:source.replace('export function createUi(','function createObservedReturnUi(')+'\nexport function createUi(ports){const owner=createObservedReturnUi(ports);return {...owner,step(view,now){globalThis.__returnView=view;const result=owner.step(view,now);if(result)globalThis.__returnSemantics=result;return result;}};}'});});
  await bootPlayableSession(page,'asd3');
  let bag=await page.evaluate(()=>__playableRuntime.gameplay().inventory);
  if(!bag.some(i=>i.typeFlags===0x9ec&&i.quantity>=2)){
   await page.evaluate(()=>{const npc=__returnView.entities.find(e=>e.kind==='npc'&&e.refObjId===2008);if(!npc)throw Error('Native accessory not in scope');__playableRuntime.session({kind:'gameplay',command:{kind:'select',gid:npc.gid}});});
   await page.locator('[data-ui-id="shop-open"]').click({timeout:15000});
   await page.waitForFunction(()=>__playableRuntime.gameplay().shop&&!__playableRuntime.gameplay().inventoryPending);
   const offer=await page.evaluate(()=>__playableRuntime.gameplay().shop.offers.find(o=>o.refObjId===61));assert.ok(offer,'native accessory Return Scroll offer');
   const gold=await page.evaluate(()=>__playableRuntime.gameplay().progression?.gold??'0');stages.push({kind:'supply',gold,offer});
   assert.ok(BigInt(gold)>=BigInt(offer.price)*2n,'asd3 needs two Return Scrolls or enough gold to buy them');
   await page.evaluate(o=>__playableRuntime.session({kind:'gameplay',command:{kind:'shop-buy',tab:o.tab,slot:o.slot,quantity:2}}),offer);
   await page.waitForFunction(()=>!__playableRuntime.gameplay().inventoryPending&&__playableRuntime.gameplay().inventory.some(i=>i.refObjId===61&&i.quantity>=2),null,{timeout:12000});
   await page.keyboard.press('Escape');bag=await page.evaluate(()=>__playableRuntime.gameplay().inventory);
  }
  stages.push({kind:'inventory',bag});
  const item=bag.find(i=>i.typeFlags===0x9ec&&i.quantity>=2);assert.ok(item,'scratch character needs at least two ordinary Return Scrolls');
  stages.push({kind:'before',item});await page.keyboard.press('KeyI');
  const slot=page.locator('[data-ui-id="slot:'+item.slot+'"]');await slot.waitFor();
  await slot.dblclick();
  await page.waitForFunction(()=>!__playableRuntime.gameplay().inventoryPending,null,{timeout:12000});
  const cast=await page.evaluate(()=>__playableRuntime.gameplay().returnScroll);stages.push({kind:'used',cast});
  await page.waitForFunction(()=>globalThis.__returnSemantics?.controls.some(c=>c.id==='return-cancel')&&__returnView.simulationTimeMs-__playableRuntime.gameplay().returnScroll.startedAtMs>=1500,null,{timeout:10000});
  await page.screenshot({path:out+'/casting.png'});
  assert.ok(cast);assert.equal(cast.durationMs,item.tooltip.fields.itemParam1_29c);
  await page.locator('[data-ui-id="return-cancel"]').click();await page.waitForFunction(()=>!__playableRuntime.gameplay().returnScroll,null,{timeout:10000});
  const afterCancel=await page.evaluate(slot=>__playableRuntime.gameplay().inventory.find(i=>i.slot===slot)?.quantity??0,item.slot);assert.equal(afterCancel,item.quantity-1);
  stages.push({kind:'cancelled',quantity:afterCancel});await page.screenshot({path:out+'/cancelled.png'});
  await slot.dblclick();await page.waitForFunction(()=>!!__playableRuntime.gameplay().returnScroll,null,{timeout:10000});
  await page.waitForFunction(()=>!__playableRuntime.gameplay()?.returnScroll,null,{timeout:45000});
  await page.waitForFunction(()=>__playableRuntime.sessionState()?.phase==='world'&&__playableRuntime.gameplay()?.localGid>0&&/Frontend: world\n/.test(document.querySelector('output')?.textContent),null,{timeout:45000});
  await page.waitForFunction(()=>globalThis.__returnSemantics?.loadingVisible!==true&&!__returnView.travel&&__returnView.worldReady&&__returnSemantics.controls.some(c=>c.id==='open-window:Inventory'),null,{timeout:60000});
  const after=await page.evaluate(()=>({pose:__playableRuntime.gameplay().pose,inventory:__playableRuntime.gameplay().inventory,session:__playableRuntime.sessionState()}));
  assert.ok(wire.some(f=>f.opcode===0x3449),'public return effect packet');assert.ok(wire.filter(f=>f.opcode===0x3369).length>=2,'native reset at admission and return');assert.equal(after.inventory.find(i=>i.slot===item.slot)?.quantity??0,item.quantity-2);stages.push({kind:'returned',...after});await page.screenshot({path:out+'/returned.png'});assert.deepEqual(errors,[]);
 }finally{await writeFile(out+'/incident.json',JSON.stringify({stages,errors,wire},null,2));await browser.close();if(original)await resetMissionMovementFixture({session:authority,characterName:'asd3',timeoutMs:30000,fixture:{id:'return-scroll-restore-v1',movementMode:3,start:original,startYawRadians:(original.angle??0)*Math.PI*2/65536}});}
});
