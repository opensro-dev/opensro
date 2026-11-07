import {test} from 'node:test';
import assert from 'node:assert/strict';
import {mkdir,writeFile} from 'node:fs/promises';
import {launchProbeBrowser} from '../../../../scripts/lib/probeBrowser.mjs';
import {bootPlayableSession} from './helpers/playable-session.mjs';

test('live GPU help follows native hover targets, state changes and popup retirement',{timeout:180000},async()=>{
 const {browser,page}=await launchProbeBrowser(),directory='temp/artifacts/tooltips-live',stages=[],errors=[];
 await mkdir(directory,{recursive:true});page.on('pageerror',error=>errors.push(String(error)));
 try{
  await page.route('**/src/engine/runtime/ui/ui.ts',async route=>{const response=await route.fetch(),source=await response.text();const body=source.replace('export function createUi(','function createObservedUi(')+`\nexport function createUi(ports){const publish=ports.publish;ports.publish=scene=>{globalThis.__tooltipScene=scene;publish(scene)};const owner=createObservedUi(ports);globalThis.__tooltipOwner=owner;return {...owner,event(event){if(event.kind==='hover')globalThis.__tooltipHover=event.id;return owner.event(event)},step(...input){const result=owner.step(...input);if(result)globalThis.__tooltipSemantics=result;return result;}};}`;await route.fulfill({response,body,contentType:'application/javascript'});});
  await bootPlayableSession(page,'asd2');
  if(await page.locator('[data-ui-id="rebirth-point"]').count()){await page.locator('[data-ui-id="rebirth-point"]').click();await page.locator('[data-ui-id="rebirth-point"]').waitFor({state:'detached',timeout:30000});}
  const open=async(key,panel)=>{await page.keyboard.press(key);await page.waitForFunction(panel=>__tooltipOwner.stats().panel===panel&&!__tooltipSemantics?.loadingVisible&&__tooltipOwner.stats().pending===0&&__tooltipOwner.stats().windowMissing.length===0,panel,{timeout:45000});assert.deepEqual(await page.evaluate(()=>__tooltipOwner.stats().failed),[]);};
  const hover=async id=>{await page.locator('[data-ui-id="'+id+'"]').hover();await page.waitForFunction(id=>__tooltipHover===id&&__tooltipScene?.quads.some(q=>q.texture.endsWith('com_tooltip_corner.png')),id,{timeout:10000});await page.screenshot({path:directory+'/'+id.replaceAll(':','-')+'.png'});stages.push({id,stats:await page.evaluate(()=>__tooltipOwner.stats())});};
  await open('KeyI','Inventory');await hover('equipment-view');await hover('select-window:Skills');
  const itemId=await page.evaluate(()=>__tooltipSemantics.controls.find(c=>c.id.startsWith('slot:')&&c.draggable)?.id);assert.ok(itemId,'scratch character owns an item to inspect');await hover(itemId);
  await open('KeyS','Skills');
  // Owner selection can precede publication while the new window's textures load.
  await page.locator('[data-ui-id^="mastery-info:"]').first().waitFor({timeout:15000});
  const masteryId=await page.evaluate(()=>__tooltipSemantics.controls.find(c=>c.id.startsWith('mastery-info:'))?.id);assert.ok(masteryId);await hover(masteryId);
  const groupIds=await page.evaluate(()=>__tooltipSemantics.controls.filter(c=>c.id.startsWith('skill-group:')).map(c=>c.id));assert.ok(groupIds.length);for(const id of groupIds)await hover(id);
  const skillId=await page.evaluate(()=>__tooltipSemantics.controls.find(c=>/^skill:\d+$/.test(c.id))?.id);assert.ok(skillId);await hover(skillId);
  await page.keyboard.press('Escape');await page.waitForFunction(()=>!__tooltipScene.quads.some(q=>q.texture.endsWith('com_tooltip_corner.png')));
  await open('KeyA','Actions');await hover('action:1000');await hover('action:1001');
  await page.mouse.move(5,5);await page.waitForFunction(()=>!__tooltipScene.quads.some(q=>q.texture.endsWith('com_tooltip_corner.png')));
  assert.deepEqual(errors,[]);
 }finally{await writeFile(directory+'/incident.json',JSON.stringify({stages,errors},null,2));await browser.close();}
});
