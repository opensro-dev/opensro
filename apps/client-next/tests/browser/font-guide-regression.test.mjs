import {test} from 'node:test';
import assert from 'node:assert/strict';
import {mkdir,writeFile} from 'node:fs/promises';
import {launchProbeBrowser} from '../../../../scripts/lib/probeBrowser.mjs';
import {bootPlayableSession} from './helpers/playable-session.mjs';

// Observe the actual font decoder and GPU texture admission. Loose files and
// accessible labels alone cannot prove the packed glyph image matches its UVs.
test('font atlas and guide remain coherent through authenticated boot and reload',{timeout:180000},async()=>{
 const out='temp/artifacts/bugs/font-guide-audit/'+(process.env.SRO_FONT_AUDIT_CAPTURE??'current');await mkdir(out,{recursive:true});
 const {browser,page}=await launchProbeBrowser(),errors=[],stages=[],requests=[];let failure;
 page.on('pageerror',e=>errors.push(e.message));
 page.on('request',r=>{const p=new URL(r.url()).pathname;if(p.startsWith('/assets/packs/'))requests.push(p);});
 try{
  await page.route('**/foundation/rendering/ui-glyphs.ts*',async route=>{
   const response=await route.fetch(),source=await response.text();assert.match(source,/export function decodeUiFont\(value\)/);
   await route.fulfill({response,body:source.replace('export function decodeUiFont(value)','function decodeObservedFont(value)')+'\nexport function decodeUiFont(value){const font=decodeObservedFont(value);globalThis.__fontAudit={image:font.image,width:font.atlasWidth,height:font.atlasHeight,digit:font.fonts["0"].glyphs["50"]};return font;}'});
  });
  await page.route('**/renderer/device/ui.ts*',async route=>{
   const response=await route.fetch(),source=await response.text();assert.match(source,/function texture\(id, image\) \{/);
   await route.fulfill({response,body:source.replace('function texture(id, image) {','function texture(id, image) { if(image&&id.includes("native-ui-font-atlas"))globalThis.__fontAuditTexture={id,width:image.width,height:image.height};')});
  });
  for(const phase of ['cold','reload']){
   const requestStart=requests.length;
   console.log('[font-guide]',phase,'authenticated boot');await bootPlayableSession(page,'asd2');
   await page.context().tracing.start({screenshots:true,snapshots:true});
   await page.waitForFunction(()=>globalThis.__fontAudit&&globalThis.__fontAuditTexture,null,{timeout:20000});
   const font=await page.evaluate(()=>({descriptor:__fontAudit,texture:__fontAuditTexture}));
   const stage={phase,font};stages.push(stage);
   await page.screenshot({path:out+'/'+phase+'-hud.png'});
   await page.keyboard.press('KeyH');await page.locator('[data-ui-id="guide-sidebar"]').click();await page.locator('[data-ui-id="guide-tab:quests"]').waitFor({timeout:20000});
   const labels=()=>page.locator('[data-ui-id^="guide-"]').evaluateAll(es=>es.map(e=>e.getAttribute('aria-label')||e.textContent));
   await page.waitForFunction(()=>[...document.querySelectorAll('[data-ui-id^="guide-"]')].some(e=>(e.getAttribute('aria-label')||e.textContent||'').includes('Play guide')),null,{timeout:20000});
   const help=await labels();await page.screenshot({path:out+'/'+phase+'-help.png'});
   await page.locator('[data-ui-id="guide-tab:quests"]').click();
   await page.waitForTimeout(300);const quests=await labels();await page.screenshot({path:out+'/'+phase+'-quests.png'});
   Object.assign(stage,{help,quests});
   stage.packRequests=requests.slice(requestStart);
   assert.deepEqual([font.texture.width,font.texture.height],[font.descriptor.width,font.descriptor.height],'GPU font image must match glyph atlas dimensions');
   assert.ok(help.includes('Guild system'),'corrected English Help caption reaches the live guide');
   for(const title of ['Jangan','Donwhang','Hotan','Taklamakan'])assert.ok(quests.includes(title),title+' reaches the live guide');
   assert.ok(!quests.includes('0')&&!quests.includes(''),'no placeholder quest group captions');
   await page.context().tracing.stop({path:out+'/'+phase+'-trace.zip'});
   await page.evaluate(()=>__playableRuntime.session({kind:'logout'}));await page.waitForFunction(()=>__playableRuntime.sessionState()?.phase==='signed-out');
  }
  assert.deepEqual(errors,[]);
 }catch(error){failure=error;}
 finally{
  await page.context().tracing.stop({path:out+'/failure-trace.zip'}).catch(()=>{});
  await page.evaluate(()=>__playableRuntime?.session({kind:'logout'})).catch(()=>{});await browser.close();
  await writeFile(out+'/incident.json',JSON.stringify({verdict:failure?'FAIL':'PASS SUCCESS',failure:failure?.stack,errors,stages},null,2));
 }
 if(failure)throw failure;
});
