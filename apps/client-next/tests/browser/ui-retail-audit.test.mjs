import {test} from 'node:test';
import assert from 'node:assert/strict';
import {mkdir,writeFile} from 'node:fs/promises';
import {launchProbeBrowser} from '../../../../scripts/lib/probeBrowser.mjs';
import {bootPlayableSession} from './helpers/playable-session.mjs';
test('retail main-popup navigation, tooltips and independent academy entry',{timeout:180000},async()=>{
 const {browser,page}=await launchProbeBrowser(),directory='temp/artifacts/bugs/verified';await mkdir(directory,{recursive:true});
 const errors=[],stages=[];page.on('pageerror',e=>errors.push(String(e)));
 try{
  await page.route('**/src/engine/runtime/ui/ui.ts',async route=>{
   const response=await route.fetch(),source=await response.text();
   const body=source.replace('export function createUi(','function createObservedUi(')+`\nexport function createUi(ports){const publish=ports.publish;ports.publish=scene=>{globalThis.__uiRetailScene=scene;publish(scene)};const owner=createObservedUi(ports);globalThis.__uiRetail=owner;return {...owner,step(...input){const result=owner.step(...input);if(result)globalThis.__uiRetailSemantics=result;return result;}};}`;
   await route.fulfill({response,body,contentType:'application/javascript'});
  });
  await bootPlayableSession(page,'asd2');
  if(await page.locator('[data-ui-id="rebirth-point"]').count()){await page.locator('[data-ui-id="rebirth-point"]').click();await page.locator('[data-ui-id="rebirth-point"]').waitFor({state:'detached',timeout:30000});await page.waitForFunction(()=>/Frontend: world\n/.test(document.querySelector('output')?.textContent),null,{timeout:45000});}
  for(const [key,panel] of [['KeyC','Character'],['KeyI','Inventory'],['KeyS','Skills'],['KeyP','Party'],['KeyL','Academy']]){
   await page.keyboard.press(key);await page.waitForFunction(panel=>__uiRetail.stats().panel===panel&&__uiRetail.stats().windowMissing.length===0,panel,{timeout:20000});
   await page.waitForFunction(panel=>!__uiRetailSemantics?.loadingVisible&&__uiRetailSemantics?.controls?.some(c=>c.id==='main-popup-drag'&&c.label===(panel==='Skills'?'Skill':panel)),panel,{timeout:20000});
   await page.waitForFunction(()=>__uiRetail.stats().pending===0,null,{timeout:45000});
   assert.deepEqual(await page.evaluate(()=>__uiRetail.stats().failed),[],'authored UI textures must be available');
   await page.evaluate(()=>new Promise(resolve=>requestAnimationFrame(()=>requestAnimationFrame(resolve))));
   await page.screenshot({path:directory+'/'+panel+'.png'});
   const ids=await page.locator('[data-ui-id]').evaluateAll(es=>es.map(e=>e.dataset.uiId));assert.equal(new Set(ids).size,ids.length);
   stages.push({panel,ids});
   if(panel==='Inventory'){
    await page.locator('[data-ui-id="select-window:Character"]').hover();
    await page.waitForFunction(()=>__uiRetailScene.quads.some(q=>q.texture.endsWith('com_tooltip_corner.png')));
    await page.screenshot({path:directory+'/sidebar-tooltip.png'});
    await page.locator('[data-ui-id="inventory-gold"]').click();await page.locator('[data-ui-id="gold-amount"]').waitFor();
    await page.waitForFunction(()=>__uiRetailScene.quads.some(q=>q.texture.endsWith('mini_gold_icon.png')));
    assert.deepEqual(await page.locator('[data-ui-id="gold-amount"]').evaluate(e=>[e.selectionStart,e.selectionEnd]),[0,1]);
    await page.screenshot({path:directory+'/gold-drop.png'});await page.keyboard.press('Escape');
   }
  }
  await page.locator('[data-ui-id="hud-menu"]').click();await page.waitForFunction(()=>__uiRetail.stats().panel==='');
  await page.locator('[data-ui-id="hud-menu"]').click();await page.waitForFunction(()=>__uiRetail.stats().panel==='Academy');
  assert.equal(await page.locator('[data-ui-id="hotbar-clear"]').count(),0);
  await page.screenshot({path:directory+'/menu-reopened.png'});
  assert.deepEqual(errors,[]);
 }finally{await writeFile(directory+'/incident.json',JSON.stringify({stages,errors},null,2));await browser.close();}
});
