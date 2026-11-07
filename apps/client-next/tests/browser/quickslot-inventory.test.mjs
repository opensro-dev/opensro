import {test} from 'node:test';
import assert from 'node:assert/strict';
import {mkdir} from 'node:fs/promises';
import {launchProbeBrowser} from '../../../../scripts/lib/probeBrowser.mjs';
import {CLIENT_NEXT_BASE_URL} from '../../../../scripts/lib/probeEndpoints.mjs';

test('F1 potion labels and keyboard activation follow authoritative inventory repairs',{timeout:45000},async()=>{
 const {browser,page}=await launchProbeBrowser();try{
  await page.setViewportSize({width:1200,height:900});await page.goto(CLIENT_NEXT_BASE_URL);
  await page.waitForFunction(()=>document.querySelector('output')?.textContent?.includes('runtime: running'));
  await page.evaluate(async()=>{
   (await import('/src/bootstrap.ts')).runtime.dispose();
   const {createAssets}=await import('/src/engine/runtime/assets/assets.ts'),{createUi}=await import('/src/engine/runtime/ui/ui.ts'),{createRenderer}=await import('/src/engine/runtime/renderer/renderer.ts'),{createPlatform}=await import('/src/engine/runtime/platform/platform.ts'),{createGameplay}=await import('/src/engine/runtime/simulation/worker/session/world/gameplay/gameplay.ts');
   const assets=createAssets(),renderer=createRenderer(document.querySelector('canvas')),commands=[],frames=[],g=createGameplay(f=>frames.push(f));
   g.bootstrap({inventorySlotCount:58,equipmentSlotCount:13,character:{hp:100,mp:100,maxHp:100,maxMp:1000,quickSlots:[{slot:9,kind:0x46,payload:0},{slot:10,kind:0x46,payload:1}]},refItemSnapshot:[
    {refObjId:1,typeFlags:0x8ec,name:'HP potion',icon:'item/etc/hp_potion_01.ddj',nativeFields:{itemParam1_29c:100}},
    {refObjId:2,typeFlags:0x10ec,name:'MP potion',icon:'item/etc/mp_potion_01.ddj',nativeFields:{itemParam3_2a4:100}},
    {refObjId:3,typeFlags:0x10ec,name:'Large MP potion',icon:'item/etc/mp_potion_02.ddj',nativeFields:{itemParam3_2a4:500}},
    {refObjId:4,typeFlags:0x6c,name:'Return scroll'}],equipItems:[[13,1,50],[14,2,1],[15,2,50],[16,3,50],[17,4,1]].map(([slot,id,n])=>({slot,refObjId:id,body:[id,0,0,0,n,0]}))});
   const local={gid:1,countryByte9c:0,regionId:257,x:0,y:0,z:0,heading:0,appearanceState:[1,0,0]};g.seed(local);g.step(0,local);
   let semantics;const ui=createUi({assets,commands:c=>commands.push(c),publish:s=>renderer.setUi(s),texture:(id,image)=>renderer.setUiTexture(id,image),base:location.origin,apiBase:'http://fixture.invalid'});
   const platform=createPlatform(document.querySelector('canvas'),document.querySelector('output'),()=>{},()=>{},()=>{},ui.event,ui.blocks);
   const state={session:{phase:'world',revision:1,character:'Fixture'},gameplay:g.take(),entities:[],width:1200,height:900,worldReady:true};
   globalThis.quickslotFixture={commands,frames,get semantics(){return semantics;},draw(){const next=ui.step(state,performance.now());if(next){semantics=next;platform.presentUi(next);}renderer.frame({width:1200,height:900});},receive(opcode,payload){g.receive({opcode,payload:Uint8Array.from(payload)},performance.now());state.gameplay=g.take();this.draw();},dispose(){g.dispose();ui.dispose();platform.dispose();assets.dispose();renderer.dispose();}};
   quickslotFixture.draw();
  });
  await page.waitForFunction(()=>{quickslotFixture.draw();return quickslotFixture.semantics?.controls.some(c=>c.id==='hotbar:10');});
  await page.evaluate(()=>{quickslotFixture.receive(0xb06d,[1,0,13,17,0,0,0]);quickslotFixture.receive(0xb5bd,[1,14,0,0,0xec,0x10]);});
  await page.waitForFunction(()=>{quickslotFixture.draw();return quickslotFixture.semantics.controls.find(c=>c.id==='hotbar:10')?.label.includes('Large MP potion x50');});
  assert.match(await page.locator('[data-ui-id="hotbar:9"]').getAttribute('aria-label'),/HP potion x50/);
  await page.keyboard.press('F1');await page.keyboard.press('Digit9');await page.keyboard.press('Digit0');
  assert.deepEqual(await page.evaluate(()=>quickslotFixture.commands),[{kind:'gameplay',command:{kind:'item-use',slot:17}},{kind:'gameplay',command:{kind:'item-use',slot:16}}]);
  await mkdir('temp/artifacts/quickslot-inventory',{recursive:true});await page.screenshot({path:'temp/artifacts/quickslot-inventory/rebound-potions.png'});
  await page.evaluate(()=>quickslotFixture.dispose());
 }finally{await browser.close();}
});
