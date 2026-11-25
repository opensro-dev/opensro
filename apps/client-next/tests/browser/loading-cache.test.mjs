import {test} from 'node:test';
import assert from 'node:assert/strict';
import {mkdir,writeFile} from 'node:fs/promises';
import {launchProbeBrowser} from '../../../../scripts/lib/probeBrowser.mjs';
import {CLIENT_NEXT_BASE_URL} from '../../../../scripts/lib/probeEndpoints.mjs';
import {resolveProbeCredentials} from '../../../../scripts/lib/probeSession.mjs';
import {holdProbeRuntime} from './helpers/hold-runtime.mjs';
test('real browser refresh reuses verified startup payloads',{timeout:180000},async t=>{
 const {browser,page}=await launchProbeBrowser({viewport:{width:1024,height:768}});t.after(()=>browser.close());
 await page.addInitScript(()=>{const Original=window.Worker;window.Worker=class extends Original{constructor(...args){super(...args);this.addEventListener('message',event=>{if(event.data.kind==='progress')window.__assetProgress=event.data.progress;});}};});
 // Routing disables Chromium's HTTP cache. Observe the unmodified network,
 // including worker traffic, so this proves real reload behavior.
 const passes=[[],[]],measurements=[{requests:[]},{requests:[]}],pending=[];let pass=0;
 page.context().on('request',request=>{if(new URL(request.url()).pathname.startsWith('/assets/'))passes[pass].push(new URL(request.url()).pathname);});
 page.context().on('requestfinished',request=>{const row=measurements[pass];if(new URL(request.url()).pathname.startsWith('/assets/'))pending.push((async()=>{const response=await request.response();row.requests.push({url:request.url(),status:response.status(),range:request.headers().range??null,...await request.sizes()});})());});
 await mkdir('temp/artifacts/loading-cache',{recursive:true});
 for(pass=0;pass<2;pass++){
  console.info('startup cache pass '+pass);const start=performance.now();if(pass===0)await page.goto(CLIENT_NEXT_BASE_URL);else await page.reload();
  await page.locator('[data-ui-id="frontend:reveal"]').waitFor({timeout:60000});await Promise.all(pending);measurements[pass].revealMs=performance.now()-start;measurements[pass].revealBytes=measurements[pass].requests.reduce((n,r)=>n+r.responseBodySize,0);
  await page.locator('[data-ui-id="frontend:reveal"]').click();await page.locator('[data-ui-id="account"]').waitFor({timeout:60000});measurements[pass].loginMs=performance.now()-start;
  measurements[pass].progress=await page.evaluate(()=>window.__assetProgress);await page.screenshot({path:'temp/artifacts/loading-cache/startup-'+pass+'.png'});
 }
 pass=1;const persistent=await page.evaluate(async()=>{let count=0;for(const name of await caches.keys()){if(name.startsWith('sro-next-verified-'))count+=(await (await caches.open(name)).keys()).length;}return count;});
 await Promise.all(pending);await writeFile('temp/artifacts/loading-cache/startup.json',JSON.stringify({passes,persistent,measurements},null,2));
 assert.ok(persistent>0);const cold=passes[0].filter(p=>p!=='/assets/packs/manifest.json'),warm=passes[1].filter(p=>p!=='/assets/packs/manifest.json');
 console.info(JSON.stringify({cold:cold.length,warm:warm.length,persistent,measurements:measurements.map(({requests,...rest})=>({...rest,assetBodyBytes:requests.reduce((n,r)=>n+r.responseBodySize,0)}))}));
 // UI bootstrap images may use ordinary browser requests. Decoded scene payloads
 // are served through the worker cache and must not be downloaded again.
 assert.equal(warm.filter(p=>/\.(?:glb|json\.gz|bin|texture)$/.test(p)).length,0,JSON.stringify(warm));
 assert.equal(measurements[1].requests.reduce((n,r)=>n+r.responseBodySize,0),0,'reload must not retransmit asset bodies, including the manifest');
 assert.ok(warm.length<cold.length);assert.ok(measurements[1].progress.bytesReceived<4096,'only manifest revalidation metadata may transfer; cache reads are not downloads');assert.ok(measurements[1].progress.cacheHits>0);
});

test('both race transitions present loading artwork before customization',{timeout:180000},async t=>{
 const {browser,page}=await launchProbeBrowser({viewport:{width:1024,height:768}});t.after(()=>browser.close());await holdProbeRuntime(page);const control=id=>page.locator('[data-ui-id="'+id+'"]');
 await page.route('**/character/list',async route=>{const response=await route.fetch(),body=await response.json();await route.fulfill({response,json:{...body,characters:body.characters.filter(row=>row.name!=='asd').slice(0,3)}});});
 await page.route('**/character/create',route=>route.abort());await page.route('**/character/delete-action',route=>route.abort());
 await page.goto(CLIENT_NEXT_BASE_URL);await control('frontend:reveal').click({timeout:60000});await control('login').waitFor();await page.waitForFunction(()=>!document.querySelector('[data-ui-id="login"]')?.disabled);
 const {loginId,loginPassword}=resolveProbeCredentials();await control('account').fill(loginId);await control('password').fill(loginPassword);await control('password').press('Enter');await control('frontend:create').waitFor({timeout:60000});await control('frontend:create').click();
 for(const [race,x] of [['europe',260],['china',720]]){
  console.info('loading journey '+race);await page.waitForFunction(()=>/Frontend: create\n/.test(document.querySelector('output')?.textContent),null,{timeout:40000});await page.mouse.click(x,280);
  await page.waitForFunction(()=>/Frontend: loading-create/.test(document.querySelector('output')?.textContent),null,{timeout:20000});
  const png=(await page.screenshot({path:'temp/artifacts/loading-cache/'+race+'-loading.png'})).toString('base64');
  const raster=await page.evaluate(async({png,race})=>{
   const shot=await createImageBitmap(await(await fetch('data:image/png;base64,'+png)).blob()),reference=await createImageBitmap(await(await fetch('/assets/images/Media_extracted/interface/loading/loading_charactercustom'+(race==='europe'?'_europe':'')+'.png')).blob());
   const canvas=document.createElement('canvas');canvas.width=1024;canvas.height=768;const ctx=canvas.getContext('2d');ctx.drawImage(shot,0,0);const actual=ctx.getImageData(200,100,600,480).data;ctx.fillStyle='black';ctx.fillRect(0,0,1024,768);ctx.drawImage(reference,0,0,1024,768);const expected=ctx.getImageData(200,100,600,480).data;let error=0,contrast=0;for(let i=0;i<actual.length;i+=4)for(let k=0;k<3;k++){error+=Math.abs(actual[i+k]-expected[i+k]);contrast+=expected[i+k];}shot.close();reference.close();return {meanError:error/(600*480*3),contrast:contrast/(600*480*3)};
  },{png,race});
  await writeFile('temp/artifacts/loading-cache/'+race+'-raster.json',JSON.stringify(raster));assert.ok(raster.contrast>5);assert.ok(raster.meanError<3,JSON.stringify(raster));
  await control('create:name').waitFor({timeout:60000});await page.waitForFunction(()=>!document.querySelector('[data-ui-id="create:name"]')?.disabled);
  await page.screenshot({path:'temp/artifacts/loading-cache/'+race+'-ready.png'});await control('frontend:back').click();
 }
});
