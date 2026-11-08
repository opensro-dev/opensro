import {test} from 'node:test';
import assert from 'node:assert/strict';
import {mkdir,writeFile} from 'node:fs/promises';
import {launchProbeBrowser} from '../../../../scripts/lib/probeBrowser.mjs';
import {bootPlayableSession} from './helpers/playable-session.mjs';

// Explicit operator opt-in: this test creates one scroll if the selected GM
// has none. Authentication and every command use the production owners.
test('authenticated GM console creates and picks up a native speed scroll',{skip:process.env.SRO_GM_ITEM_GRANT!=='1',timeout:120000},async()=>{
 const character=process.env.SRO_PROBE_CHARACTER;
 assert.ok(character,'explicit GM character required');
 const {browser,page}=await launchProbeBrowser(),directory='temp/artifacts/gm-item-live',errors=[];
 await mkdir(directory,{recursive:true});page.on('pageerror',e=>errors.push(String(e)));
 try{
  await page.route('**/src/engine/runtime/ui/ui.ts',async route=>{
   const response=await route.fetch(),source=await response.text();
   const body=source.replace('export function createUi(','function createObservedUi(')+`\nexport function createUi(ports){const owner=createObservedUi(ports);globalThis.__gmItemProbe={};return {...owner,step(view,now){globalThis.__gmItemProbe.view=view;return owner.step(view,now);}};}`;
   await route.fulfill({response,body,contentType:'application/javascript'});
  });
  await bootPlayableSession(page,character);
  assert.equal(await page.evaluate(()=>__playableRuntime.gameplay().eligibility?.gm),true);
  await page.keyboard.press('Shift+Backquote');
  const input=page.locator('[data-ui-id="gm-input"]');await input.waitFor({timeout:10000});
  const before=await page.evaluate(()=>__playableRuntime.gameplay().inventory.filter(r=>r.refObjId===24198));
  let gid;
  if(!before.length){
   await input.fill('/MAKEITEM ITEM_ETC_SPEED_UP_BASIC 1');await input.press('Enter');
   await page.waitForFunction(()=>__gmItemProbe.view.entities.some(e=>e.groundItem&&e.refObjId===24198),null,{timeout:15000});
   gid=await page.evaluate(()=>__gmItemProbe.view.entities.find(e=>e.groundItem&&e.refObjId===24198).gid);
   await page.keyboard.press('Escape');
   await page.evaluate(gid=>__playableRuntime.session({kind:'gameplay',command:{kind:'pickup',gid}}),gid);
   await page.waitForFunction(()=>__playableRuntime.gameplay().inventory.some(r=>r.refObjId===24198),null,{timeout:15000});
  }else await page.keyboard.press('Escape');
  const result=await page.evaluate(()=>({session:__playableRuntime.sessionState().character,gm:__playableRuntime.gameplay().eligibility.gm,scrolls:__playableRuntime.gameplay().inventory.filter(r=>r.refObjId===24198)}));
  await page.keyboard.press('KeyI');await page.screenshot({path:directory+'/inventory.png'});
  await writeFile(directory+'/incident.json',JSON.stringify({character,before,gid,result,errors},null,2));
  assert.deepEqual(errors,[]);
 }catch(error){await page.screenshot({path:directory+'/failure.png'}).catch(()=>{});await writeFile(directory+'/failure.json',JSON.stringify({error:String(error),errors},null,2));throw error;}
 finally{await browser.close();}
});
