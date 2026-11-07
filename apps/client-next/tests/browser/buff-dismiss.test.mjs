import {test} from 'node:test';
import assert from 'node:assert/strict';
import {mkdir,writeFile} from 'node:fs/promises';
import {launchProbeBrowser} from '../../../../scripts/lib/probeBrowser.mjs';
import {CLIENT_NEXT_BASE_URL} from '../../../../scripts/lib/probeEndpoints.mjs';
test('Buff board uses native right-release and confirmation through the real platform bridge',{timeout:90000},async()=>{
 const {browser,page}=await launchProbeBrowser();
 try{
 await page.setViewportSize({width:1200,height:900});await page.goto(CLIENT_NEXT_BASE_URL);
 await page.waitForFunction(()=>document.querySelector('output')?.textContent?.includes('runtime: running'));
  await page.evaluate(async()=>{
   const entry=Array.from(document.scripts).find(s=>s.src&&new URL(s.src).pathname==='/src/bootstrap.ts');
   const {runtime}=await import(entry.src);runtime.dispose();
   const {createUi}=await import('/src/engine/runtime/ui/ui.ts'),{createRenderer}=await import('/src/engine/runtime/renderer/renderer.ts'),{createPlatform}=await import('/src/engine/runtime/platform/platform.ts');
   const jobs=new Map(),requests=[],commands=[],renderer=createRenderer(document.querySelector('canvas'));let serial=0,scene=null;
   const assets={available:()=>Math.max(0,32-jobs.size),request(url,limit,kind){const id=++serial;jobs.set(id,null);requests.push(url);fetch(url).then(async r=>{if(!r.ok)throw Error(r.status+' '+url);const result=kind==='png'?{kind:'image',image:await createImageBitmap(await r.blob())}:{kind:'bytes',buffer:await r.arrayBuffer()};if(jobs.has(id))jobs.set(id,result);else if(result.kind==='image')result.image.close();}).catch(e=>{if(jobs.has(id))jobs.set(id,{kind:'error',error:String(e)});});return id;},take(id){const result=jobs.get(id);if(result)jobs.delete(id);return result;},cancel(id){const result=jobs.get(id);if(result?.kind==='image')result.image.close();jobs.delete(id);}};
   let platform;const ui=createUi({assets,commands:c=>commands.push(c),publish:s=>{scene=s;renderer.setUi(s);},texture:(id,image)=>renderer.setUiTexture(id,image),base:location.origin,apiBase:'http://fixture.invalid',clickSound:()=>{},sound:()=>{},loadingVariant:()=>1,chooseTip:()=>0,saveOptions:value=>platform.saveGameOptions(value),saveBlocks:value=>platform.saveChatBlocks(value)});
   platform=createPlatform(document.querySelector('canvas'),document.querySelector('output'),()=>{},()=>{},()=>{},ui.event,ui.blocks);
   const state={session:{phase:'world',revision:1,character:'Fixture',characters:[{id:1,name:'Fixture',level:1,maxHp:200,maxMp:200}]},gameplay:{localGid:1,inventory:[],inventorySlotCount:45,equipmentSlotCount:13,vitals:[{gid:1,hp:200,mp:200}],casts:[]},entities:[],width:1200,height:900,worldReady:true};
   window.flagFixture={ui,renderer,platform,state,requests,commands,get scene(){return scene;},get pending(){return jobs.size;},restorePlatform(){platform.dispose();platform=createPlatform(document.querySelector('canvas'),document.querySelector('output'),()=>{},()=>{},()=>{},ui.event,ui.blocks);this.platform=platform;},draw(){const semantic=ui.step({...state},performance.now());if(semantic)platform.presentUi(semantic);renderer.frame({width:state.width,height:state.height});}};
   state.gameplay.skillCatalog=[{id:7,name:'Speed',icon:'item/etc/hp_potion_01.ddj',buffCancel:'confirm'}];state.gameplay.buffSlots=[{state:'active',serial:1,secondary:false,effect:{gid:1,skill:7,token:99,phase:1}}];flagFixture.draw();
  });
  await page.waitForFunction(()=>{flagFixture.draw();return flagFixture.pending===0;},null,{timeout:20000});

  const icon=page.locator('[data-ui-id="buff:99:7"]');await icon.waitFor();
  await icon.click();await icon.dblclick();await page.evaluate(()=>flagFixture.draw());
  assert.equal(await page.locator('[data-ui-id="buff-dismiss-confirm"]').count(),0);
  const box=await icon.boundingBox();await page.mouse.move(box.x+10,box.y+10);await page.mouse.down({button:'right'});await page.evaluate(()=>flagFixture.draw());
  assert.equal(await page.locator('[data-ui-id="buff-dismiss-confirm"]').count(),0,'RMB down only arms the native board');
  await page.mouse.up({button:'right'});await page.waitForFunction(()=>{flagFixture.draw();return !!document.querySelector('[data-ui-id="buff-dismiss-confirm"]');});
  await mkdir('temp/artifacts/buff-dismiss',{recursive:true});await page.screenshot({path:'temp/artifacts/buff-dismiss/confirmation.png'});
  await page.keyboard.press('Escape');await page.evaluate(()=>flagFixture.draw());assert.deepEqual(await page.evaluate(()=>flagFixture.commands),[]);
  await icon.click({button:'right'});await page.evaluate(()=>flagFixture.draw());await page.locator('[data-ui-id="buff-dismiss-confirm"]').click();await page.evaluate(()=>flagFixture.draw());
  assert.deepEqual(await page.evaluate(()=>flagFixture.commands),[{kind:'gameplay',command:{kind:'effect-cancel',skillId:7,token:0}}]);assert.equal(await icon.count(),1,'until server teardown the icon remains');
  await writeFile('temp/artifacts/buff-dismiss/interaction.json',JSON.stringify({result:'PASS',lane:'real browser input with deterministic gameplay fixture',commands:await page.evaluate(()=>flagFixture.commands)},null,2));
 }finally{await browser.close();}
});
