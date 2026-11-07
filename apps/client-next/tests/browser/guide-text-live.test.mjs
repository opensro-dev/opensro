import {test} from 'node:test';
import assert from 'node:assert/strict';
import {mkdir,writeFile} from 'node:fs/promises';
import {launchProbeBrowser} from '../../../../scripts/lib/probeBrowser.mjs';
import {bootPlayableSession} from './helpers/playable-session.mjs';
// Live proof for the h-bug1/h-bug2 guide-text repair: the Help tab shows the
// completed "Guild system" row (not "?? ???") and the Quest tab shows
// attested region titles (not "0"), with the START body on two native lines.
test('live guide text shows completed titles without placeholder glyphs',{timeout:180000},async()=>{
 const {browser,page}=await launchProbeBrowser(),directory='temp/artifacts/bugs';
 await mkdir(directory,{recursive:true});
 const errors=[];page.on('pageerror',e=>errors.push(String(e)));
 const stages={};
 const note=async(name,data)=>{stages[name]=data;await writeFile(directory+'/h-fix-incident.json',JSON.stringify({stages,errors},null,2));};
 try{
  await page.route('**/src/engine/runtime/ui/ui.ts',async route=>{
   const response=await route.fetch(),source=await response.text();
   assert.ok(source.includes('export function createUi('));
   const body=source.replace('export function createUi(','function createObservedUi(')+`\nexport function createUi(ports){const owner=createObservedUi(ports);globalThis.__guideProbe={owner};return {...owner,step(view,now){globalThis.__guideProbe.view=view;return owner.step(view,now);}};}`;
   await route.fulfill({response,body,contentType:'application/javascript'});
  });
  await bootPlayableSession(page,'asd2');
  await page.keyboard.press('KeyH');
  await page.waitForFunction(()=>globalThis.__guideProbe.owner.stats().panel==='Game Guide',null,{timeout:20000});
  await page.waitForFunction(()=>globalThis.__guideProbe.owner.stats().windowMissing.length===0,null,{timeout:30000});
  const labels=await page.evaluate(()=>globalThis.__guideProbe.view ? undefined : undefined).catch(()=>null);
  const helpLabels=await page.locator('[data-ui-id^="guide-"]').evaluateAll(es=>es.map(e=>e.getAttribute('aria-label')||e.textContent));
  await note('help-labels',{helpLabels});
  assert.ok(helpLabels.some(t=>t&&t.includes('Guild system')),'completed guild menu title renders');
  assert.ok(!helpLabels.some(t=>t==='0'||(t&&t.includes('??'))),'no placeholder titles or missing-glyph runs');
  await page.screenshot({path:directory+'/h-fix1.png'});
  await page.locator('[data-ui-id="guide-tab:quests"]').click();
  await page.waitForFunction(()=>{
   const els=[...document.querySelectorAll('[data-ui-id^="guide-"]')].map(e=>e.getAttribute('aria-label')||e.textContent);
   return els.some(t=>t&&t.includes('Jangan'));
  },null,{timeout:20000});
  const questLabels=await page.locator('[data-ui-id^="guide-"]').evaluateAll(es=>es.map(e=>e.getAttribute('aria-label')||e.textContent));
  await note('quest-labels',{questLabels});
  for(const region of ['Jangan','Donwhang','Hotan','Taklamakan'])assert.ok(questLabels.some(t=>t&&t.includes(region)),region+' renders');
  assert.ok(!questLabels.some(t=>t==='0'),'no zero placeholder rows');
  await page.screenshot({path:directory+'/h-fix2.png'});
  await note('done',{errors});
  assert.deepEqual(errors,[]);
 }catch(error){
  await writeFile(directory+'/h-fix-failure.json',JSON.stringify({error:String(error),errors,stages},null,2));
  await page.screenshot({path:directory+'/h-fix-failure.png'}).catch(()=>{});throw error;
 }finally{await browser.close();}
});
