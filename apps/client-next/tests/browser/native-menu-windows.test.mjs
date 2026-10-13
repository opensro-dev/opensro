import {test} from 'node:test';
import assert from 'node:assert/strict';
import {mkdir} from 'node:fs/promises';
import {launchProbeBrowser} from '../../../../scripts/lib/probeBrowser.mjs';
import {CLIENT_NEXT_BASE_URL} from '../../../../scripts/lib/probeEndpoints.mjs';
const {panelCloseId}=await import('../../src/engine/foundation/ui/panels.ts');
import {assertNativeStretchRaster} from './stretch-raster.mjs';

test('native menu window audit uses real assets over a visible background',{timeout:45000},async()=>{
 const {browser,page}=await launchProbeBrowser();try{
  await page.setViewportSize({width:1200,height:900});await page.goto(CLIENT_NEXT_BASE_URL);await page.waitForFunction(()=>document.querySelector('output')?.textContent?.includes('runtime: running'));
  await page.evaluate(async()=>{
   const {runtime}=await import('/src/bootstrap.ts');runtime.dispose();
   const {createAssets}=await import('/src/engine/runtime/assets/assets.ts'),{createUi}=await import('/src/engine/runtime/ui/ui.ts'),{createRenderer}=await import('/src/engine/runtime/renderer/renderer.ts'),{createPlatform}=await import('/src/engine/runtime/platform/platform.ts');
   const assets=createAssets(),renderer=createRenderer(document.querySelector('canvas')),commands=[],sounds=[];let scene,semantics;
   const ui=createUi(assets,c=>commands.push(c),s=>{scene=s;renderer.setUi(s?{...s,quads:[{rect:[0,0,1200,900],clip:[0,0,1200,900],texture:'',uv:[0,0,1,1],color:[.08,.4,.15,1]},...s.quads]}:null);},(id,image)=>renderer.setUiTexture(id,image),location.origin,'http://fixture.invalid',()=>sounds.push('click'),kind=>sounds.push(kind));
   const platform=createPlatform(document.querySelector('canvas'),document.querySelector('output'),()=>{},()=>{},()=>{},ui.event,ui.blocks);
   const state={session:{phase:'world',revision:1,character:'Fixture'},gameplay:{localGid:1,inventorySlotCount:45,equipmentSlotCount:13,inventoryPending:false,inventory:[{slot:14,refObjId:3630,typeFlags:0x8ec,quantity:10,name:'HP recovery herb',icon:'item/etc/hp_potion_01.ddj',magic:[]}],progression:{level:30,gold:"12345",experience:"1234",skillPoints:200,masteries:[{id:257,level:10},{id:258,level:1},{id:259,level:1},{id:273,level:1},{id:274,level:1},{id:275,level:1}]},skills:[],vitals:[],casts:[]},entities:[],width:1200,height:900,worldReady:true};
   globalThis.inventoryFixture={state,commands,sounds,event:ui.event,stats:ui.stats,get scene(){return scene;},get semantics(){return semantics;},draw(){const next=ui.step(state,performance.now());if(next){semantics=next;platform.presentUi(next);}renderer.frame({width:1200,height:900});},dispose(){ui.dispose();platform.dispose();assets.dispose();renderer.dispose();}};
   inventoryFixture.draw();ui.event({kind:'activate',id:'open-window:Inventory'});
  });
  await page.waitForFunction(()=>{inventoryFixture.draw();return inventoryFixture.semantics?.controls.some(c=>c.id==='slot:14');});
  await page.waitForFunction(()=>{inventoryFixture.draw();const cover=document.getElementById('startup-loading');return !cover||getComputedStyle(cover).opacity==='0';});
  await mkdir('temp/artifacts/ui-window-audit',{recursive:true});
  for(const panel of ['Inventory','Character','Skills','Actions','Party','Quests']){
   if(panel!=='Inventory')await page.locator('[data-ui-id="select-window:'+panel+'"]').click();
   await page.waitForFunction(()=>{inventoryFixture.draw();return !inventoryFixture.stats().windowMissing.length;});
   await page.waitForTimeout(300);await page.evaluate(()=>inventoryFixture.draw());
   const screenshot=await page.screenshot({path:'temp/artifacts/ui-window-audit/'+panel.toLowerCase()+'.png'});
   if(panel==='Inventory'){
    const origin=await page.evaluate(async()=>{const r=inventoryFixture.semantics.controls.find(c=>c.id==='main-popup-drag').rect;const layout=await (await fetch('/assets/cif/layouts/ifmainpopup.json')).json(),pane=layout.controlsByName.GDR_INVENTORY.rect;return [r[0]-10+pane.x,r[1]+pane.y];});
    const foreground=await page.evaluate(()=>inventoryFixture.scene.quads.filter(q=>q.texture.startsWith('/assets/fonts/')).map(q=>q.rect));
    await assertNativeStretchRaster(page,screenshot,[{id:'ifinventory/Create/GDR_INVENTORY_LATTICE_OUTLINE',origin}],foreground);
    const holes=await page.evaluate(async png=>{
     const image=await createImageBitmap(new Blob([Uint8Array.from(atob(png),c=>c.charCodeAt(0))],{type:'image/png'}));
     const canvas=new OffscreenCanvas(image.width,image.height),ctx=canvas.getContext('2d');ctx.drawImage(image,0,0);image.close();
     const {data}=ctx.getImageData(0,0,canvas.width,canvas.height),background=(300*canvas.width+500)*4;let count=0;
     for(let y=460;y<815;y++)for(let x=825;x<1188;x++){const at=(y*canvas.width+x)*4;if(data[at]===data[background]&&data[at+1]===data[background+1]&&data[at+2]===data[background+2])count++;}
     return count;
    },screenshot.toString('base64'));
    assert.equal(holes,0,'Inventory interior must not expose the green world through its outline');
   }
   if(panel==='Skills'){
    const pixels=await page.evaluate(async png=>{
     const footer=inventoryFixture.scene.quads.find(q=>q.texture.endsWith('/skill/skl_wnd_box.png'));
     if(!footer)throw Error('Missing native Skill point footer');
     const source=await createImageBitmap(await (await fetch(footer.texture)).blob()),shot=await createImageBitmap(new Blob([Uint8Array.from(atob(png),c=>c.charCodeAt(0))],{type:'image/png'}));
     const canvas=new OffscreenCanvas(shot.width,shot.height),ctx=canvas.getContext('2d');ctx.drawImage(shot,0,0);
     const actual=ctx.getImageData(footer.rect[0],footer.rect[1],source.width,source.height).data;
     ctx.clearRect(0,0,canvas.width,canvas.height);ctx.drawImage(source,0,0);
     const expected=ctx.getImageData(0,0,source.width,source.height).data;let compared=0,mismatched=0;
     for(let y=0;y<source.height;y++)for(let x=0;x<source.width;x++){
      if(x>=12&&x<source.width-12&&y>=4&&y<source.height-4)continue;
      const i=(y*source.width+x)*4;if(expected[i+3]!==255)continue;compared++;
      if([0,1,2].some(c=>Math.abs(actual[i+c]-expected[i+c])>1))mismatched++;
     }
     source.close();shot.close();return {compared,mismatched};
    },screenshot.toString('base64'));
    assert.ok(pixels.compared>2000);assert.equal(pixels.mismatched,0,'Skill point border must retain native art instead of being overwritten by the earlier frame');
   }
   if(panel==='Character'){
    const controls=await page.evaluate(()=>inventoryFixture.semantics.controls.filter(c=>c.id==='stat-str'||c.id==='stat-int'));
    assert.equal(controls.length,2,'Native stat buttons remain visible with no available points');assert.ok(controls.every(c=>c.disabled));
   }
  }
  await page.evaluate(()=>{inventoryFixture.event({kind:'activate',id:'open-window:Skills'});inventoryFixture.draw();});
  for(const id of ['skill-mastery:257','skill-mastery:258','skill-mastery:259','skill-tab:1','skill-mastery:273','skill-mastery:274','skill-mastery:275','skill-mastery:276','skill-mastery:513','skill-mastery:515','skill-tab:1','skill-mastery:514','skill-mastery:516','skill-tab:2','skill-mastery:517','skill-mastery:518']){
   if(id==='skill-mastery:276')await page.evaluate(()=>{inventoryFixture.state.gameplay.progression.masteries.push({id:276,level:0});inventoryFixture.draw();});
   if(id==='skill-mastery:513')await page.evaluate(()=>{inventoryFixture.state.gameplay.progression.masteries=[513,514,515,516,517,518].map(id=>({id,level:0}));inventoryFixture.event({kind:'activate',id:'skill-tab:0'});inventoryFixture.draw();});
   await page.evaluate(id=>{inventoryFixture.event({kind:'activate',id});inventoryFixture.draw();},id);
   await page.waitForFunction(()=>{inventoryFixture.draw();return !inventoryFixture.stats().windowMissing.length;});
   const shot=await page.screenshot({path:'temp/artifacts/ui-window-audit/'+id.replace(':','-')+'.png'});
   const holes=await page.evaluate(async png=>{const image=await createImageBitmap(new Blob([Uint8Array.from(atob(png),c=>c.charCodeAt(0))],{type:'image/png'}));const canvas=new OffscreenCanvas(image.width,image.height),ctx=canvas.getContext('2d');ctx.drawImage(image,0,0);image.close();const {data}=ctx.getImageData(0,0,canvas.width,canvas.height),sample=(300*canvas.width+500)*4;let holes=0;for(let y=498;y<815;y++)for(let x=838;x<1175;x++){const at=(y*canvas.width+x)*4;if(data[at]===data[sample]&&data[at+1]===data[sample+1]&&data[at+2]===data[sample+2])holes++;}return holes;},shot.toString('base64'));
   assert.equal(holes,0,id+' exposes world pixels inside Skills');
  }
  await page.evaluate(close=>{inventoryFixture.event({kind:'activate',id:close});inventoryFixture.draw();inventoryFixture.sounds.length=0;},panelCloseId('Skills'));
  for(const key of ['c','s','i','a','p','q','q']){await page.keyboard.press(key);await page.evaluate(()=>inventoryFixture.draw());}
  assert.deepEqual(await page.evaluate(()=>inventoryFixture.sounds),['open','open','open','open','open','open','close'],'real C/S/I/A/P/Q keys must route every admitted transition sound');
  await page.evaluate(()=>inventoryFixture.dispose());
 }finally{await browser.close();}
});
