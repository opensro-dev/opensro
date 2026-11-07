import {test} from 'node:test';
import assert from 'node:assert/strict';
import {mkdir} from 'node:fs/promises';
import {launchProbeBrowser} from '../../../../scripts/lib/probeBrowser.mjs';
import {bootPlayableSession} from './helpers/playable-session.mjs';

test('native equipment overlays render without taking away item interaction',{timeout:180000},async()=>{
 const {browser,page}=await launchProbeBrowser();const errors=[];page.on('pageerror',e=>errors.push(String(e)));
 await mkdir('temp/artifacts/equipment-overlays',{recursive:true});
 try{
  // Inject only the presentation fixture. No authority inventory is changed.
  await page.route('**/src/engine/runtime/ui/ui.ts',async route=>{
   const response=await route.fetch(),source=await response.text();
   const body=source.replace('export function createUi(','function createObservedUi(')+`
export function createUi(ports){const publish=ports.publish;ports.publish=scene=>{globalThis.__overlayScene=scene;publish(scene)};const owner=createObservedUi(ports);globalThis.__overlayOwner=owner;let mode,game;
return {...owner,step(next,now){if(next.gameplay&&globalThis.__overlayMode){if(mode!==globalThis.__overlayMode||game?.original!==next.gameplay){mode=globalThis.__overlayMode;const inventory=next.gameplay.inventory.map(item=>item.slot===6?{...item,durability:mode==='broken'?0:mode==='warning'?6:30,tooltip:{...item.tooltip,fields:{...item.tooltip?.fields,reqLevelType1:1,requiredLevel:mode==='denied'?999:0,reqLevelType2:0,reqLevelType3:0,reqLevelType4:0,reqStr:0,reqInt:0,reqGender:2,country:3,maxDurability:50}}}:item);game={original:next.gameplay,value:{...next.gameplay,inventory}};}next={...next,gameplay:game.value};}const result=owner.step(next,now);if(result)globalThis.__overlaySemantics=result;return result;}};}`;
   await route.fulfill({response,body,contentType:'application/javascript'});
  });
  await bootPlayableSession(page,'asd2');
  if(await page.locator('[data-ui-id="rebirth-point"]').count()){await page.locator('[data-ui-id="rebirth-point"]').click();await page.locator('[data-ui-id="rebirth-point"]').waitFor({state:'detached',timeout:30000});}
  await page.keyboard.press('KeyI');
  await page.locator('[data-ui-id="slot:6"]').waitFor({timeout:45000});
  for(const [mode,texture] of [['denied','icon_disable.png'],['broken','icon_item_broken.png'],['warning','icon_item_warning.png']]){
   await page.evaluate(mode=>globalThis.__overlayMode=mode,mode);
   await page.waitForFunction(texture=>__overlayScene?.quads.some(q=>q.texture.endsWith(texture)),texture,{timeout:30000});
   const control=await page.evaluate(()=>__overlaySemantics.controls.find(c=>c.id==='slot:6'));
   assert.equal(control.disabled,false);assert.equal(control.draggable,true);
   await page.locator('[data-ui-id="slot:6"]').hover();
   await page.screenshot({path:'temp/artifacts/equipment-overlays/'+mode+'.png'});
  }
  const uv=await page.evaluate(()=>JSON.stringify(__overlayScene.quads.find(q=>q.texture.endsWith('icon_item_warning.png')).uv));
  await page.waitForFunction(uv=>JSON.stringify(__overlayScene.quads.find(q=>q.texture.endsWith('icon_item_warning.png'))?.uv)!==uv,uv);
  await page.evaluate(()=>globalThis.__overlayMode='repaired');
  await page.waitForFunction(()=>!__overlayScene.quads.some(q=>q.texture.endsWith('icon_item_warning.png')));
  assert.deepEqual(await page.evaluate(()=>__overlayOwner.stats().failed),[]);assert.deepEqual(errors,[]);
 }finally{await browser.close();}
});
