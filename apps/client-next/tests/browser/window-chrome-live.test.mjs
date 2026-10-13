import {test} from 'node:test';
import assert from 'node:assert/strict';
import {mkdir,writeFile} from 'node:fs/promises';
import {launchProbeBrowser} from '../../../../scripts/lib/probeBrowser.mjs';
import {bootPlayableSession} from './helpers/playable-session.mjs';
const {panelCloseId}=await import('../../src/engine/foundation/ui/panels.ts');

// Real pointer input (hit-testing, drag capture, overlap) for service-window
// chrome. Complements the synthetic ui.event() sweep in ui-behavior.test.mjs,
// which cannot prove a strip is hittable at its authored rect.
test('live window chrome drags through real pointer input',{timeout:300000},async()=>{
 const {browser,page}=await launchProbeBrowser();
 const directory='temp/artifacts/window-chrome';await mkdir(directory,{recursive:true});
 const errors=[];page.on('pageerror',e=>errors.push(String(e)));
 const stages={};
 const note=async(name,data)=>{stages[name]=data;await writeFile(directory+'/incident.json',JSON.stringify({stages,errors},null,2));};
 try{
  await page.route('**/src/engine/runtime/ui/ui.ts',async route=>{
   const response=await route.fetch(),source=await response.text();
   assert.ok(source.includes('export function createUi('));
   const body=source.replace('export function createUi(','function createObservedUi(')+`\nexport function createUi(...args){const owner=createObservedUi(...args);globalThis.__chromeProbe={owner};return {...owner,step(view,now){globalThis.__chromeProbe.view=view;return owner.step(view,now);}};}`;
   await route.fulfill({response,body,contentType:'application/javascript'});
  });
  await bootPlayableSession(page,'asd2');
  const panel=()=>page.evaluate(()=>globalThis.__chromeProbe.owner.stats().panel);
  const box=id=>page.locator(`[data-ui-id="${id}"]`).boundingBox();
  const dragBy=(id,dx,dy)=>{
   return page.evaluate(async({id,dx,dy})=>{
    const el=document.querySelector(`[data-ui-id="${CSS.escape(id)}"]`);if(!el)throw Error('missing control '+id);
    const r=el.getBoundingClientRect(),cx=r.x+r.width/2,cy=r.y+r.height/2;
    return {cx,cy};
   },{id,dx,dy}).then(async({cx,cy})=>{await page.mouse.move(cx,cy);await page.mouse.down();await page.mouse.move(cx+dx,cy+dy,{steps:10});await page.mouse.up();});
  };
  // Actions tab through the main-popup strip (shared placement).
  await page.keyboard.press('KeyA');
  await page.locator('[data-ui-id="main-popup-drag"]').waitFor({timeout:20000});
  const actionsBefore=await box('main-popup-drag');
  await dragBy('main-popup-drag',-60,40);
  const actionsAfter=await box('main-popup-drag');
  await page.screenshot({path:directory+'/actions-dragged.png'});
  await note('actions',{before:actionsBefore,after:actionsAfter});
  assert.equal(Math.round(actionsAfter.x-actionsBefore.x),-60);
  assert.equal(Math.round(actionsAfter.y-actionsBefore.y),40);
  assert.deepEqual([actionsAfter.width,actionsAfter.height],[367,34]);
  await page.keyboard.press('Escape');
  await page.waitForFunction(()=>globalThis.__chromeProbe.owner.stats().panel==='',null,{timeout:10000});
  // Alchemy admits live (space-path image) and drags through its own strip.
  // Default binding index 24 ('Alchemy') is KeyY; KeyE is Party Matching.
  await page.keyboard.press('KeyY');
  await page.locator('[data-ui-id="window-drag:Alchemy"]').waitFor({timeout:20000});
  const alchemyBefore=await box('window-drag:Alchemy');
  await dragBy('window-drag:Alchemy',-50,30);
  const alchemyAfter=await box('window-drag:Alchemy');
  await page.screenshot({path:directory+'/alchemy-dragged.png'});
  await note('alchemy',{before:alchemyBefore,after:alchemyAfter,panel:await panel()});
  assert.equal(Math.round(alchemyAfter.x-alchemyBefore.x),-50);
  assert.equal(Math.round(alchemyAfter.y-alchemyBefore.y),30);
  assert.equal(await panel(),'Alchemy');
  // Alchemy opens the real companion inventory; a COS is not required.
  await page.locator('[data-ui-id="companion-close"]').waitFor({timeout:10000});
  const companionBefore=await box('main-popup-drag'),alchemyFixed=await box('window-drag:Alchemy');
  await dragBy('main-popup-drag',-35,-20);
  const companionAfter=await box('main-popup-drag');
  assert.equal(Math.round(companionAfter.x-companionBefore.x),-35);
  assert.equal(Math.round(companionAfter.y-companionBefore.y),-20);
  assert.deepEqual(await box('window-drag:Alchemy'),alchemyFixed,'companion has independent placement');
  await note('companion',{before:companionBefore,after:companionAfter,alchemyFixed});

  await page.locator(`[data-ui-id="${panelCloseId('Alchemy')}"]`).click();
  await page.waitForFunction(()=>globalThis.__chromeProbe.owner.stats().panel==='',null,{timeout:10000});
  // Options tabs keep the shared frame origin; the title strip drags.
  await page.keyboard.press('Escape');
  await page.locator('[data-ui-id="open-window:Option"]').waitFor({timeout:10000});
  await page.locator('[data-ui-id="open-window:Option"]').click();
  await page.locator('[data-ui-id="window-drag:Option"]').waitFor({timeout:20000});
  const origins=[];
  for(let tab=0;tab<5;tab++){
   await page.locator(`[data-ui-id="option-tab:${tab}"]`).click();
   const r=await box('window-drag:Option');origins.push([Math.round(r.x),Math.round(r.y)]);
  }
  await page.screenshot({path:directory+'/options-tabs.png'});
  await note('options-tabs',{origins});
  assert.ok(origins.every(([x,y])=>x===origins[0][0]&&y===origins[0][1]));
  const optionBefore=await box('window-drag:Option');
  await dragBy('window-drag:Option',40,-30);
  const optionAfter=await box('window-drag:Option');
  await note('options-drag',{before:optionBefore,after:optionAfter});
  assert.equal(Math.round(optionAfter.x-optionBefore.x),40);
  assert.equal(Math.round(optionAfter.y-optionBefore.y),-30);
  await page.keyboard.press('Escape');
  await page.waitForFunction(()=>globalThis.__chromeProbe.owner.stats().panel==='',null,{timeout:10000});
  await note('done',{errors});
  assert.deepEqual(errors,[]);
 }catch(error){
  await writeFile(directory+'/failure.json',JSON.stringify({error:String(error),errors,stages,state:await page.evaluate(()=>({session:globalThis.__playableRuntime?.sessionState(),panel:globalThis.__chromeProbe?.owner.stats()})).catch(()=>null)},null,2));
  await page.screenshot({path:directory+'/failure.png'}).catch(()=>{});throw error;
 }finally{await browser.close();}
});
