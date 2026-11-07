import {test} from 'node:test';
import assert from 'node:assert/strict';
import {mkdir} from 'node:fs/promises';
import {launchProbeBrowser} from '../../../../scripts/lib/probeBrowser.mjs';
import {CLIENT_NEXT_BASE_URL} from '../../../../scripts/lib/probeEndpoints.mjs';

test('party matching paints all twelve empty retail bars without inventing selectable records',{timeout:45000},async()=>{
 const {browser,page}=await launchProbeBrowser();
 try{
  await page.setViewportSize({width:1200,height:900});await page.goto(CLIENT_NEXT_BASE_URL);
  await page.waitForFunction(()=>document.querySelector('output')?.textContent?.includes('runtime: running'));
  await page.evaluate(async()=>{
   const {runtime}=await import('/src/bootstrap.ts');runtime.dispose();
   const {createAssets}=await import('/src/engine/runtime/assets/assets.ts'),{createUi}=await import('/src/engine/runtime/ui/ui.ts'),{createRenderer}=await import('/src/engine/runtime/renderer/renderer.ts'),{createPlatform}=await import('/src/engine/runtime/platform/platform.ts');
   const assets=createAssets(),renderer=createRenderer(document.querySelector('canvas'));let scene,semantics;
   const commands=[];const ui=createUi({assets,commands:c=>commands.push(c),publish:value=>{scene=value;renderer.setUi(value);},texture:(id,image)=>renderer.setUiTexture(id,image),base:location.origin,apiBase:'http://fixture.invalid'});
   const platform=createPlatform(document.querySelector('canvas'),document.querySelector('output'),()=>{},()=>{},()=>{},ui.event,ui.blocks);
   const state={session:{phase:'world',revision:1,character:'Fixture'},gameplay:{localGid:1,inventory:[],vitals:[],casts:[],social:{localName:'Fixture',self:1,leader:0,options:3,members:[]},partyMatching:{auto:[],page:0,pages:1,rows:[],own:null,request:null,pending:null,result:null}},entities:[],width:1200,height:900,worldReady:true};
   globalThis.matchingFixture={state,commands,get scene(){return scene;},get semantics(){return semantics;},draw(){const next=ui.step(state,performance.now());if(next){semantics=next;platform.presentUi(next);}renderer.frame({width:1200,height:900});},dispose(){ui.dispose();platform.dispose();assets.dispose();renderer.dispose();}};
   matchingFixture.draw();ui.event({kind:'activate',id:'open-window:Party Matching'});
  });
  await page.waitForFunction(()=>{matchingFixture.draw();return matchingFixture.scene?.quads.filter(q=>q.texture.endsWith('com_bar01_left.png')).length===12;});
  await page.waitForFunction(()=>{matchingFixture.draw();const cover=document.getElementById('startup-loading');return !cover||getComputedStyle(cover).opacity==='0';});
  const result=await page.evaluate(()=>({rows:matchingFixture.scene.quads.filter(q=>q.texture.endsWith('com_bar01_left.png')).map(q=>q.rect),selectable:matchingFixture.semantics.controls.filter(c=>c.id.startsWith('party-match-row:')).length}));
  assert.equal(result.selectable,0);assert.equal(new Set(result.rows.map(r=>r[1])).size,12);
  await mkdir('temp/artifacts/matching-empty',{recursive:true});await page.screenshot({path:'temp/artifacts/matching-empty/party.png'});
  for(const job of [1,2,3,4]){
   await page.evaluate(job=>{matchingFixture.state.gameplay.inventory=job===4?[]:[{slot:8,refObjId:100,typeFlags:0x3ac|(job<<11),quantity:1,magic:[]}];matchingFixture.draw();},job);
   await page.locator('[data-ui-id="party-match:18"]').click();
   await page.waitForFunction(()=>{matchingFixture.draw();return matchingFixture.semantics.controls.some(c=>c.id==='party-form-title');});
   for(let purpose=0;purpose<4;purpose++)assert.equal(await page.locator('[data-ui-id="party-form-purpose:'+purpose+'"]').isDisabled(),!(job===4?purpose<2:job===2?purpose===3:purpose===2));
   await page.locator('[data-ui-id="party-form-cancel"]').click();await page.evaluate(()=>matchingFixture.draw());
  }
  await page.locator('[data-ui-id="party-match:18"]').click();
  await page.waitForFunction(()=>{matchingFixture.draw();return matchingFixture.semantics.controls.some(c=>c.id==='party-form-title');});
  await page.locator('[data-ui-id="party-form-title"]').fill('Hunting');await page.evaluate(()=>matchingFixture.draw());
  await page.screenshot({path:'temp/artifacts/matching-empty/register.png'});
  await page.locator('[data-ui-id="party-form-confirm"]').click();await page.evaluate(()=>matchingFixture.draw());
  assert.equal(await page.evaluate(()=>matchingFixture.commands.at(-1).command.kind),'party-match-register');
  await page.locator('[data-ui-id="party-match:17"]').click();await page.waitForFunction(()=>{matchingFixture.draw();return matchingFixture.semantics.controls.some(c=>c.id==='party-form-auto:race:2');});
  await page.screenshot({path:'temp/artifacts/matching-empty/auto.png'});
  await page.keyboard.press('Escape');await page.evaluate(()=>{matchingFixture.state.gameplay.partyMatching.request={a:11,b:42,primary:257,secondary:273,flags:4,member:{id:77,name:'Peer',level:30,model:1907,region:25000},expires:performance.now()+10000};matchingFixture.draw();});
  await page.waitForFunction(()=>{matchingFixture.draw();return matchingFixture.semantics.controls.some(c=>c.id==='party-answer:1');});
  await page.waitForFunction(()=>{matchingFixture.draw();return ['mastery_sword.png','mastery_cold.png'].every(name=>matchingFixture.scene.quads.some(q=>q.texture.endsWith(name)));});
  await page.screenshot({path:'temp/artifacts/matching-empty/join-request.png'});
  await page.locator('[data-ui-id="party-answer:1"]').click();assert.deepEqual(await page.evaluate(()=>matchingFixture.commands.at(-1).command),{kind:'party-match-answer',a:11,b:42,answer:1});
  await page.evaluate(()=>matchingFixture.dispose());
 }finally{await browser.close();}
});
