import {test} from 'node:test';
import assert from 'node:assert/strict';
import {mkdir,writeFile} from 'node:fs/promises';
import {launchProbeBrowser} from '../../../../scripts/lib/probeBrowser.mjs';
import {CLIENT_NEXT_BASE_URL} from '../../../../scripts/lib/probeEndpoints.mjs';
test('crests decode through the asset worker and retained local timers advance',{timeout:90000},async()=>{
 const {browser,page}=await launchProbeBrowser();
 try{
 await page.route('**/marks/*.crb',route=>route.fulfill({status:200,contentType:'application/octet-stream',body:Buffer.alloc(256,1)}));
 await page.setViewportSize({width:1200,height:900});await page.goto(CLIENT_NEXT_BASE_URL);
 await page.waitForFunction(()=>document.querySelector('output')?.textContent?.includes('runtime: running'));
  await page.evaluate(async()=>{
   localStorage.removeItem('sro:v1150:game-options:1');
   const entry=Array.from(document.scripts).find(s=>s.src&&new URL(s.src).pathname==='/src/bootstrap.ts');
   const {runtime}=await import(entry.src);runtime.dispose();
   const {createUi}=await import('/src/engine/runtime/ui/ui.ts'),{createRenderer}=await import('/src/engine/runtime/renderer/renderer.ts'),{createPlatform}=await import('/src/engine/runtime/platform/platform.ts');
   const {createAssets}=await import('/src/engine/runtime/assets/assets.ts');
   const ownedAssets=createAssets(),requests=[],commands=[],renderer=createRenderer(document.querySelector('canvas'));let scene=null;
   const assets={...ownedAssets,request(url,limit,decode){requests.push(url);return ownedAssets.request(url,limit,decode);}};
   let platform;const ui=createUi({assets,commands:c=>commands.push(c),publish:s=>{scene=s;renderer.setUi(s);},texture:(id,image)=>renderer.setUiTexture(id,image),base:location.origin,apiBase:'http://fixture.invalid',clickSound:()=>{},sound:()=>{},loadingVariant:()=>1,chooseTip:()=>0,saveOptions:value=>platform.saveGameOptions(value)});
   platform=createPlatform(document.querySelector('canvas'),document.querySelector('output'),()=>{},()=>{},()=>{},ui.event,ui.blocks);
   const state={simulationTimeMs:1000,frontend:{phase:'world'},session:{crestPrefix:1,marksBase:location.origin+'/api',phase:'world',revision:1,character:'Fixture',characters:[{id:1,name:'Fixture',level:1,maxHp:200,maxMp:200}]},gameplay:{localGid:1,inventory:[],inventorySlotCount:45,equipmentSlotCount:13,vitals:[{gid:1,hp:200,mp:200}],casts:[]},entities:[],width:1200,height:900,worldReady:true};
   window.flagFixture={ui,renderer,platform,state,requests,commands,cosTimer:null,get scene(){return scene;},get pending(){return 4-assets.available();},ownedAssets,draw(){const semantic=ui.step({...state},performance.now());if(semantic)platform.presentUi(semantic);renderer.frame({width:state.width,height:state.height});}};
   const local={gid:1,kind:'local-player',name:'Fixture',regionId:257,x:0,y:0,z:0};
   const peer={...local,gid:2,kind:'player',name:'Peer',guildName:'Guild',guildGrantName:'Officer',guildId:10,guildCrests:[2,20,3]};
   state.entities=[local,peer];state.gameplay.social={localName:'Fixture',self:11,leader:12,options:0,members:[{id:11,name:'Fixture',model:1907,status:170},{id:12,name:'Peer',model:1907,status:133}],guild:{id:10,name:'Guild',members:[{name:'Fixture',grant:'Master'}]}};
   state.gameplay.attachedEffects=[{gid:2,skill:3,token:123,phase:2},{gid:1,skill:3,token:124,phase:2,durationMs:10000,remainingMs:5000,receivedAtMs:1000}];state.gameplay.buffSlots=state.gameplay.attachedEffects.filter(e=>e.gid===1).map((effect,serial)=>({state:'active',serial,effect,secondary:false}));state.gameplay.skillCatalog=[{id:3,name:'Skill',icon:'skill/china/sword_smash_a.ddj',buffSecondary:false}];
   // 0x3691 subtype 3 -> 6E6E00 kind 3: the two-bar COS summon slot.
   flagFixture.cosTimer=[{itemRefObjId:12345,remainingSec:100,packedExtra:0,receivedAtMs:1000,reference:{durationSec:100,aux:0xffffffff,icon:'skill/china/sword_smash_a.ddj',name:'Pet Skill'}}];state.gameplay.cosWindows=flagFixture.cosTimer;
   state.gameplay.vitals.push({gid:2,hp:50,mp:80,abnormal:16});flagFixture.draw();
  });

 await page.waitForFunction(()=>{flagFixture.draw();return flagFixture.scene?.quads.filter(q=>q.texture.includes('/marks/')).length===2&&flagFixture.scene.quads.some(q=>q.texture.endsWith('/s_stateodd_time_gauge.png'))&&flagFixture.scene.quads.some(q=>q.texture.endsWith('/s_stateodd_time02_gauge.png'));},null,{timeout:30000});
 await mkdir('temp/artifacts/crests-war-buffs',{recursive:true});
 await page.screenshot({path:'temp/artifacts/crests-war-buffs/browser.png'});
 await page.locator('[data-ui-id="buff:124:3"]').hover();
 await page.waitForFunction(()=>{flagFixture.draw();return flagFixture.scene.quads.filter(q=>q.texture.endsWith('com_tooltip_corner.png')).length===4;},null,{timeout:30000});
 await page.screenshot({path:'temp/artifacts/crests-war-buffs/buff-tooltip.png'});
 assert.deepEqual(await page.evaluate(()=>flagFixture.scene.quads.filter(q=>/com_tooltip_(corner|edge)\.png$/.test(q.texture)).map(q=>q.sampling)),Array(8).fill('nearest'));
 assert.deepEqual(await page.evaluate(()=>flagFixture.scene.quads.filter(q=>/com_tooltip_(corner|edge)\.png$/.test(q.texture)).map(q=>q.color[3])),Array(8).fill(196/255));
 await page.mouse.move(1100,600);await page.evaluate(()=>flagFixture.draw());

 const result=await page.evaluate(()=>{
  const f=flagFixture,gauge=()=>f.scene.quads.find(q=>q.texture.endsWith('/s_stateodd_time_gauge.png'));
  const first=gauge().rect[2],marks=f.scene.quads.filter(q=>q.texture.includes('/marks/')).map(q=>({texture:q.texture,rect:q.rect}));
  const cosBar=()=>f.scene.quads.find(q=>q.texture.endsWith('/s_stateodd_time02_gauge.png'));
  const cosFirst=cosBar().rect,plainBars=f.scene.quads.filter(q=>q.texture.endsWith('/s_stateodd_time_gauge.png')).length;
  const secondBar=f.scene.quads.filter(q=>q.texture.endsWith('/s_stateodd_time_gauge.png')).map(q=>q.rect);
  f.state.simulationTimeMs=26000;f.draw();const cosAdvanced=cosBar().rect[2];
  f.state.gameplay={...f.state.gameplay,cosWindows:[]};f.draw();const cosRemoved=!cosBar();
  f.state.gameplay={...f.state.gameplay,cosWindows:f.cosTimer};f.state.simulationTimeMs=3000;f.draw();const advanced=gauge().rect[2];
  f.state.simulationTimeMs=999999;f.draw();const expired=!!document.querySelector('[data-ui-id="buff:124:3"]');
  f.state.gameplay={...f.state.gameplay,attachedEffects:[],buffSlots:[]};f.draw();const removed=!document.querySelector('[data-ui-id="buff:124:3"]');
  return {first,advanced,marks,expired,removed,cosFirst,cosAdvanced,plainBars,secondBar,cosRemoved,error:f.renderer.error(),failed:f.ui.stats().failed,health:f.ownedAssets.health()};
 });
 assert.equal(result.first,10);assert.equal(result.advanced,6);assert.equal(result.expired,true);assert.equal(result.removed,true);
 assert.equal(result.marks.length,2);assert.equal(result.marks[0].rect[2],16);assert.equal(result.marks[1].rect[0]-result.marks[0].rect[0],-16);
 // The kind-3 slot draws the 02 bar plus a second plain bar directly under it.
 assert.equal(result.cosFirst[2],20,'a fresh summon window is full');
 assert.equal(result.cosAdvanced,15,'the COS window counts down with the clock');
 assert.equal(result.plainBars,2,'the buff bar plus the COS slot second bar');
 // 6E7FA0 builds both gauges from one rect, so they overlay exactly.
 assert.deepEqual(result.secondBar[1].slice(0,2),result.cosFirst.slice(0,2),'the second bar shares the first bar rect');
 assert.equal(result.secondBar[1][3],result.cosFirst[3],'and its height');
 assert.equal(result.cosRemoved,true,'dismissing the COS retires its slot');
 assert.equal(result.error,null);assert.deepEqual(result.failed,[]);assert.equal(result.health.phase,'running');
 // Reproduce wtf-scroll.png with the actual published effect, without changing an account.
 await page.evaluate(()=>{
  const f=flagFixture;f.state.simulationTimeMs=1000;
  f.state.gameplay={...f.state.gameplay,cosWindows:[],attachedEffects:[{gid:1,skill:31107,token:125,phase:2,durationMs:3600000,remainingMs:2516000,receivedAtMs:1000}],skillCatalog:[{id:31107,name:'',icon:'item/etc/qno_ch_europe_1_02.ddj',buffSecondary:false}]};f.state.gameplay.buffSlots=f.state.gameplay.attachedEffects.map((effect,serial)=>({state:'active',serial,effect,secondary:false}));f.draw();
 });
 await page.waitForFunction(()=>{flagFixture.draw();return !!document.querySelector('[data-ui-id="buff:125:31107"]');});
 await page.locator('[data-ui-id="buff:125:31107"]').hover();
 await page.waitForFunction(()=>{flagFixture.draw();return flagFixture.scene.quads.filter(q=>q.texture.endsWith('com_tooltip_corner.png')).length===4;});
 // Wait for the new scroll icon/glyph resources to finish admission too.
 await page.waitForFunction(()=>{flagFixture.draw();return flagFixture.pending===0;});
 await page.screenshot({path:'temp/artifacts/crests-war-buffs/speed-scroll-tooltip.png'});
 assert.deepEqual(await page.evaluate(()=>flagFixture.scene.quads.filter(q=>/com_tooltip_(corner|edge)\.png$/.test(q.texture)).map(q=>q.color[3])),Array(8).fill(196/255));
 // Same texture, different sampling in the same frame: catches a bind-group
 // cache that ignores the sampler even if command admission is correct.
 const departure=await page.evaluate(()=>{
  const f=flagFixture,slot=f.state.gameplay.buffSlots[0];f.state.simulationTimeMs=1000;
  f.state.gameplay={...f.state.gameplay,attachedEffects:[],buffSlots:[{state:'departing',serial:slot.serial,effect:slot.effect,endedAtMs:1000}]};f.draw();
  const first=f.scene.quads.find(q=>q.texture.endsWith('/icon/buf_effect.png'));
  f.state.simulationTimeMs=1400;f.draw();const fourth=f.scene.quads.find(q=>q.texture.endsWith('/icon/buf_effect.png'));
  return {first,fourth,interactive:!!document.querySelector('[data-ui-id="buff:125:31107"]')};
 });
 assert.ok(departure.first&&departure.fourth,'published departure sprite reaches GPU UI');
 assert.deepEqual(departure.first.rect.slice(2),[50,52]);assert.deepEqual(departure.first.uv,[0,0,50/512,52/64]);
 assert.deepEqual(departure.fourth.uv,[200/512,0,50/512,52/64]);assert.equal(departure.fourth.color[3],170/255);assert.equal(departure.interactive,false);
 await page.screenshot({path:'temp/artifacts/crests-war-buffs/buff-departure.png'});
 const sampled=await page.evaluate(async()=>{
  const f=flagFixture,clip=[0,0,1200,900],texture='sampling-probe';
  f.renderer.setUiTexture(texture,new ImageData(new Uint8ClampedArray([255,0,0,255,0,0,255,255]),2,1));
  const q={texture,clip,uv:[0,0,1,1],color:[1,1,1,1]};
  f.renderer.setUi({revision:999999,width:1200,height:900,quads:[{...q,rect:[0,0,20,10],sampling:'nearest'},{...q,rect:[0,20,20,10],sampling:'linear'}]});
  f.renderer.frame({width:1200,height:900});
  const bitmap=await createImageBitmap(document.querySelector('canvas')),canvas=new OffscreenCanvas(20,30),context=canvas.getContext('2d');context.drawImage(bitmap,0,0);bitmap.close();
  const at=(x,y)=>Array.from(context.getImageData(x,y,1,1).data);
  return {nearestLeft:at(9,5),nearestRight:at(10,5),linear:at(9,25)};
 });
 assert.deepEqual(sampled.nearestLeft,[255,0,0,255]);assert.deepEqual(sampled.nearestRight,[0,0,255,255]);
 assert.ok(sampled.linear[0]>0&&sampled.linear[0]<255&&sampled.linear[2]>0,'linear default still interpolates');
 await writeFile('temp/artifacts/crests-war-buffs/browser.json',JSON.stringify({...result,sampled,departure},null,2));
 await page.evaluate(()=>{flagFixture.ui.dispose();flagFixture.platform.dispose();flagFixture.renderer.dispose();flagFixture.ownedAssets.dispose();});
 }finally{await browser.close();}
});
