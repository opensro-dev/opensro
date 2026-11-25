import {test} from 'node:test';
import assert from 'node:assert/strict';
import {mkdir,writeFile} from 'node:fs/promises';
import {launchProbeBrowser} from '../../../../scripts/lib/probeBrowser.mjs';
import {CLIENT_NEXT_BASE_URL} from '../../../../scripts/lib/probeEndpoints.mjs';
import {resolveProbeCredentials} from '../../../../scripts/lib/probeSession.mjs';
import {holdProbeRuntime} from './helpers/hold-runtime.mjs';

test('capture character customization materials without changing the roster',{timeout:120000},async()=>{
 const dir='temp/artifacts/character-texture';await mkdir(dir,{recursive:true});
 const {browser,page}=await launchProbeBrowser({viewport:{width:1600,height:900}}),errors=[],control=id=>page.locator(`[data-ui-id="${id}"]`);
 page.on('pageerror',e=>errors.push(e.message));
 try{
  await holdProbeRuntime(page);
  if(process.env.SRO_REFLECTION_DIAGNOSTICS==='1')await page.route('**/src/engine/runtime/characters/resources/resources.ts*',async route=>{
   const response=await route.fetch(),source=await response.text(),marker=/begin\(seconds\)\s*\{/;
   assert.match(source,marker);await route.fulfill({response,body:source.replace(marker,'begin(seconds) { globalThis.__characterResourceProbe={job,loaded:[...loaded],wanted:[...wanted],planned:[...planned],plannedBytes,reserved,available:assets.available(),failures:[...failures]};')});
  });
  await page.route('**/character/list',async route=>{const response=await route.fetch(),body=await response.json();await route.fulfill({response,json:{...body,characters:body.characters.slice(0,3)}});});
  await page.route('**/character/create',()=>{throw Error('Capture must not create a character');});
  await page.route('**/character/delete-action',()=>{throw Error('Capture must not delete a character');});
  await page.goto(CLIENT_NEXT_BASE_URL);await control('frontend:reveal').click({timeout:30000});await control('login').waitFor();
  await page.waitForFunction(()=>!document.querySelector('[data-ui-id="login"]')?.disabled);
  const {loginId,loginPassword}=resolveProbeCredentials();await control('account').fill(loginId);await control('password').fill(loginPassword);await control('password').press('Enter');
  await control('frontend:create').click({timeout:30000});await page.waitForFunction(()=>/Frontend: create\n/.test(document.querySelector('output')?.textContent));
  await page.mouse.click(500,330);await control('create:name').waitFor({timeout:40000});await page.waitForFunction(()=>!document.querySelector('[data-ui-id="create:name"]')?.disabled);
  await control('create:figure').fill('3');await control('create:weapon').fill('4');await control('create:protector').fill('2');
  await page.waitForTimeout(1500);await page.screenshot({path:dir+'/necromancer-axes.png'});
  for(const [width,height] of [[1024,768],[1280,720],[1920,1080]]){
   await page.setViewportSize({width,height});await page.waitForTimeout(800);
   assert.equal(await control('create:figure').inputValue(),'3');
   assert.equal(await control('create:weapon').inputValue(),'4');
   for(const id of ['create:name','create:figure','create:weapon','create:ok','frontend:back']){
    const box=await control(id).boundingBox();assert.ok(box&&box.x>=0&&box.y>=0&&box.x+box.width<=width&&box.y+box.height<=height,id+' outside viewport');
   }
   await page.screenshot({path:`${dir}/customization-${width}x${height}.png`});
  }
  await writeFile(dir+'/capture.json',JSON.stringify({verdict:'CAPTURED',errors,status:await page.locator('output').textContent(),viewport:page.viewportSize()},null,2));assert.deepEqual(errors,[]);
 }catch(error){await writeFile(dir+'/failure.json',JSON.stringify({error:String(error),errors,resources:await page.evaluate(()=>globalThis.__characterResourceProbe),status:await page.locator('output').textContent()},null,2));throw error;}finally{await browser.close();}
});
