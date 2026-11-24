import {test} from 'node:test';
import assert from 'node:assert/strict';
import {mkdir,writeFile} from 'node:fs/promises';
import path from 'node:path';
import {launchProbeBrowser} from '../../../../scripts/lib/probeBrowser.mjs';
import {resolveProbeCredentials,resolveProbeDivisionId} from '../../../../scripts/lib/probeSession.mjs';
import {assertCharacterAllowed} from '../../../../scripts/lib/probeCharacter.mjs';
import {AGENT_API_BASE_URL,CLIENT_PROD_PORT} from '../../../../scripts/lib/probeEndpoints.mjs';
import {serveBeta} from '../../tools/beta/serve.mjs';
import {verifyServed} from '../../tools/beta/verify.mjs';
// Runs the actual compiled application: never imports source modules or installs
// runtime probe globals. Character filtering only constrains our owned selection.
test('beta package authenticates, renders panels, restores login and retains asset cache',{timeout:300000,skip:!process.env.SRO_BETA_PACKAGE},async()=>{
 const root=process.env.SRO_BETA_PACKAGE;if(!root)throw Error('Set SRO_BETA_PACKAGE to the verified package directory');
 const character=assertCharacterAllowed(process.env.SRO_PROBE_CHARACTER??'PowerProbe',{context:'beta release validation'});
 const directory=process.env.SRO_BETA_EVIDENCE??'temp/artifacts/beta-live';await mkdir(directory,{recursive:true});
 const service=await serveBeta({root,apiTarget:AGENT_API_BASE_URL,port:Number(process.env.SRO_BETA_PORT??CLIENT_PROD_PORT)});
 let browser,page,transferPhase='initial';const transfers={initial:0,fill:0,warm:0};const errors=[],failed=[],requests=[],stages=[];
 service.server.on('request',(req,res)=>{const phase=transferPhase;res.once('finish',()=>{if(req.url.startsWith('/assets/')&&[200,206].includes(res.statusCode))transfers[phase]+=Number(res.getHeader('Content-Length')??0);});});
 const note=async(stage,detail={})=>{stages.push({stage,...detail});console.log('[beta-live]',stage);await writeFile(path.join(directory,'result.json'),JSON.stringify({releaseId:service.manifest.releaseId,stages,errors,failed},null,2));};
 try{
  await verifyServed(root,service.url);await note('served-verification');transfers.initial=0;
  ({browser,page}=await launchProbeBrowser({viewport:{width:1024,height:768}}));
  await page.context().tracing.start({screenshots:true,snapshots:true,sources:false});
  page.on('pageerror',e=>errors.push(e.message));page.on('request',r=>requests.push(new URL(r.url()).pathname));
  page.on('response',r=>{if(r.status()>=400&&!(r.status()===401&&new URL(r.url()).pathname==='/api/title/session'))failed.push({path:new URL(r.url()).pathname,status:r.status()});});
  await page.route('**/character/list',async route=>{const response=await route.fetch(),body=await response.json();assert.ok(body.characters.some(c=>c.name===character),'owned scratch exists');await route.fulfill({response,json:{...body,characters:body.characters.filter(c=>c.name===character)}});});
  const control=id=>page.locator(`[data-ui-id="${id}"]`);
  const enter=async()=>{await control('frontend:create').waitFor({timeout:60000});await page.mouse.click(505,430);await page.waitForFunction(()=>document.querySelector('[data-ui-id="enter"]')?.disabled===false,null,{timeout:20000});await control('enter').click();await page.waitForFunction(()=>/Frontend: world\n/.test(document.querySelector('output')?.textContent),null,{timeout:90000});};
  await page.goto(service.url);await control('frontend:reveal').click({timeout:60000});
  await control('native:servers').click();await control('server:'+resolveProbeDivisionId()).click();await control('native:server-accept').click();
  const {loginId,loginPassword}=resolveProbeCredentials();await control('account').fill(loginId);await control('password').fill(loginPassword);await control('password').press('Enter');
  await enter();await note('authenticated-world');await page.screenshot({path:path.join(directory,'world.png')});
  for(const [key,name]of [['KeyC','Character'],['KeyI','Inventory'],['KeyS','Skill']]){await page.keyboard.press(key);await page.waitForFunction(name=>document.querySelector('[data-ui-id="main-popup-drag"]')?.getAttribute('aria-label')===name,name,{timeout:15000});await page.screenshot({path:path.join(directory,name+'.png')});await page.keyboard.press('Escape');}
  await note('panels');
  const loginCount=requests.filter(p=>p==='/api/title/login').length;
  // Playwright routing disables HTTP caching. Remove the roster safety filter
  // after the owned actor is entered, then fill cache and measure a second reload.
  // World restoration must be automatic; never select an unfiltered roster row.
  await page.unroute('**/character/list');
  for(const phase of ['fill','warm']){transferPhase=phase;await page.reload();await page.waitForFunction(()=>/Frontend: world\n/.test(document.querySelector('output')?.textContent),null,{timeout:90000});await page.waitForFunction(()=>/UI: 0 pending images; 0 failed images/.test(document.querySelector('output')?.textContent)&&/World: .*0 pending textures/.test(document.querySelector('output')?.textContent),null,{timeout:60000});await page.waitForTimeout(500);await note('reload-'+phase,{servedAssetBytes:transfers[phase]});}
  assert.equal(requests.filter(p=>p==='/api/title/login').length,loginCount,'reload uses cookie restoration, no credential resubmission');
  await page.screenshot({path:path.join(directory,'reload.png')});
  assert.equal(await page.locator('output').isHidden(),true);assert.ok(!requests.some(p=>/^\/(src|@vite|@fs|__client-next-dev)/.test(p)));
  assert.deepEqual(errors,[]);assert.deepEqual(failed,[]);assert.equal(transfers.warm,0,'fully warm reload must not retransmit asset bodies, including publication manifest');
  await note('PASS',{servedAssetBodyBytes:transfers});
 }catch(e){await note('FAIL',{error:String(e),status:page?await page.locator('output').textContent().catch(()=>null):null});throw e;}
 finally{if(page)await page.context().tracing.stop({path:path.join(directory,'trace.zip')}).catch(()=>{});if(browser)await browser.close();await service.close();}
});
