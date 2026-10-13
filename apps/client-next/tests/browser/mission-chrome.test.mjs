import {test} from 'node:test';
import assert from 'node:assert/strict';
import {mkdir,writeFile} from 'node:fs/promises';
import {launchProbeBrowser} from '../../../../scripts/lib/probeBrowser.mjs';
import {CLIENT_NEXT_BASE_URL} from '../../../../scripts/lib/probeEndpoints.mjs';
const {panelCloseId}=await import('../../src/engine/foundation/ui/panels.ts');

async function createChromeFixture(page){
  await page.setViewportSize({width:1200,height:900});
  await page.goto(CLIENT_NEXT_BASE_URL);
  await page.waitForFunction(()=>document.querySelector('output')?.textContent?.includes('runtime: running'));
  await page.evaluate(async()=>{
   const entry=Array.from(document.scripts).find(s=>s.src&&new URL(s.src).pathname==='/src/bootstrap.ts');
   const {runtime}=await import(entry.src);runtime.dispose();
   const {createUi}=await import('/src/engine/runtime/ui/ui.ts'),{createRenderer}=await import('/src/engine/runtime/renderer/renderer.ts'),{createPlatform}=await import('/src/engine/runtime/platform/platform.ts');
   const jobs=new Map(),requests=[],commands=[],renderer=createRenderer(document.querySelector('canvas'));let serial=0,scene=null;
   const assets={available:()=>Math.max(0,32-jobs.size),request(url,limit,kind){const id=++serial;jobs.set(id,null);requests.push(url);fetch(url).then(async r=>{if(!r.ok)throw Error(r.status+' '+url);const result=kind==='png'?{kind:'image',image:await createImageBitmap(await r.blob())}:{kind:'bytes',buffer:await r.arrayBuffer()};if(jobs.has(id))jobs.set(id,result);else if(result.kind==='image')result.image.close();}).catch(e=>{if(jobs.has(id))jobs.set(id,{kind:'error',error:String(e)});});return id;},take(id){if(window.chromeFixture?.holdAssets)return null;const result=jobs.get(id);if(result)jobs.delete(id);return result;},cancel(id){const result=jobs.get(id);if(result?.kind==='image')result.image.close();jobs.delete(id);}};
   const ui=createUi(assets,c=>commands.push(c),s=>{scene=s;renderer.setUi(s);},(id,image)=>renderer.setUiTexture(id,image),location.origin,'http://fixture.invalid');
   const platform=createPlatform(document.querySelector('canvas'),document.querySelector('output'),()=>{},()=>{},()=>{},ui.event,ui.blocks);
   const state={session:{phase:'world',revision:1,character:'Fixture',characters:[{id:1,name:'Fixture',level:1,maxHp:200,maxMp:200}]},gameplay:{localGid:1,inventory:[],inventorySlotCount:45,equipmentSlotCount:13,vitals:[{gid:1,hp:200,mp:200}],casts:[]},entities:[],width:1200,height:900,worldReady:true};
   window.chromeFixture={ui,renderer,platform,state,requests,commands,get scene(){return scene;},draw(){const semantic=ui.step({...state},performance.now());if(semantic)platform.presentUi(semantic);renderer.frame({width:state.width,height:state.height});}};
   chromeFixture.draw();ui.event({kind:'activate',id:'open-window:System'});
  });
}

test('mission shared chrome uses resident retail artwork and bitmap text',{timeout:60000},async()=>{
 const {browser,page}=await launchProbeBrowser();
 try{
  await createChromeFixture(page);
  await page.waitForFunction(()=>{chromeFixture.draw();return chromeFixture.ui.stats().pending===0&&chromeFixture.scene?.quads.some(q=>q.texture.includes('mframe_wnd'));});
  await page.waitForFunction(()=>{chromeFixture.draw();const loading=document.getElementById('startup-loading');return !loading||getComputedStyle(loading).opacity==='0';});
  await mkdir('temp/artifacts/mission-chrome',{recursive:true});
  await page.screenshot({path:process.env.SRO_CHROME_BASELINE?'temp/artifacts/mission-chrome/before.png':'temp/artifacts/mission-chrome/after.png'});
  if(process.env.SRO_CHROME_BASELINE)return;
  const scene=await page.evaluate(()=>chromeFixture.scene);
  assert.ok(scene.quads.some(q=>q.texture.includes('native-ui-font')),'published font must draw world labels');
  assert.ok(!scene.quads.some(q=>q.texture==='text'),'browser font atlas must not draw mission text');
  assert.deepEqual(scene.quads.find(q=>q.texture.endsWith('ub_window_01.png')).rect,[200,848,800,52]);
  assert.deepEqual(scene.quads.find(q=>q.texture.endsWith('pmi_hp.png')).rect,[83,32,124,12]);
  assert.deepEqual(await page.locator('[data-ui-id="hotbar:1"]').boundingBox(),{x:489,y:859,width:32,height:32});
  assert.equal(await page.locator('[data-ui-id="open-window:Inventory"]').count(),0,'placeholder menu strip stays out of the persistent HUD');
  const rect=await page.locator('[data-ui-id="logout"]').boundingBox(),screenshot=(await page.screenshot()).toString('base64');
  const pixels=await page.evaluate(async({rect,screenshot})=>{
   const load=async path=>createImageBitmap(await(await fetch(path)).blob());
   const atlas=await(await fetch('/assets/fonts/native-ui-font-atlas.json')).json(),font=atlas.fonts['0'];
   const [skin,mask,capture]=await Promise.all([load('/assets/images/Media_extracted/interface/ifcommon/com_button.png'),load(atlas.image),load('data:image/png;base64,'+screenshot)]);
   const expected=new OffscreenCanvas(rect.width,rect.height),ctx=expected.getContext('2d');ctx.drawImage(skin,0,0,rect.width,rect.height);
   const glyphCanvas=new OffscreenCanvas(rect.width,rect.height),gctx=glyphCanvas.getContext('2d'),glyphs=Array.from('Sign out',c=>font.glyphs[c.codePointAt(0)]);
   let x=Math.floor((rect.width-glyphs.reduce((n,g)=>n+g.advanceX,0))/2);const baseline=Math.floor((24-font.recordHeight-5)/2)+font.ascent;
   for(const g of glyphs){gctx.drawImage(mask,g.x,g.y,g.width,g.height,x+g.originX,baseline-g.originY,g.width,g.height);x+=g.advanceX;}
   gctx.globalCompositeOperation='source-in';gctx.fillStyle='rgb(254,251,216)';gctx.fillRect(0,0,rect.width,rect.height);ctx.drawImage(glyphCanvas,0,0);
   const actual=new OffscreenCanvas(rect.width,rect.height),actx=actual.getContext('2d');actx.drawImage(capture,rect.x,rect.y,rect.width,rect.height,0,0,rect.width,rect.height);
   const a=ctx.getImageData(5,5,rect.width-10,rect.height-10).data,b=actx.getImageData(5,5,rect.width-10,rect.height-10).data;
   let max=0,total=0;for(let i=0;i<a.length;i++){const delta=Math.abs(a[i]-b[i]);max=Math.max(max,delta);total+=delta;}
   skin.close();mask.close();capture.close();return {max,mean:total/a.length,pixels:a.length/4};
  },{rect,screenshot});
  await writeFile('temp/artifacts/mission-chrome/pixel-result.json',JSON.stringify(pixels,null,2));
  assert.ok(pixels.mean<1&&pixels.max<=5,JSON.stringify(pixels));
  const requests=await page.evaluate(()=>chromeFixture.requests.length);
  await page.evaluate(()=>{chromeFixture.ui.event({kind:'hover',id:'logout'});chromeFixture.draw();});
  assert.equal(await page.evaluate(()=>chromeFixture.requests.length),requests,'hover must not request artwork');
  assert.ok(await page.evaluate(()=>chromeFixture.scene.quads.some(q=>q.texture.endsWith('com_button_focus.png'))));
  await page.evaluate(()=>{chromeFixture.ui.event({kind:'press',id:'logout'});chromeFixture.draw();});
  assert.ok(await page.evaluate(()=>chromeFixture.scene.quads.some(q=>q.texture.endsWith('com_button_press.png'))));
  await page.evaluate(()=>{chromeFixture.ui.event({kind:'hover',id:null});chromeFixture.draw();});
  assert.ok(!await page.evaluate(()=>chromeFixture.scene.quads.some(q=>q.texture.endsWith('com_button_press.png')&&q.rect[2]===330)),'drag-out releases pressed skin');
  await page.evaluate(()=>{
   chromeFixture.state.entities=[{gid:7,kind:'player',name:'SilkroadPlayer',regionId:0x6262,x:910,y:0,z:920}];
   chromeFixture.state.gameplay={...chromeFixture.state.gameplay,social:{invitation:{type:1,gid:7}}};chromeFixture.draw();
  });
  await page.waitForFunction(()=>{chromeFixture.draw();return chromeFixture.ui.stats().pending===0&&chromeFixture.scene.quads.filter(q=>q.texture.endsWith('msgbox_blackbox.png')).length===2;});
  const proposalScene=await page.evaluate(()=>chromeFixture.scene);
  assert.deepEqual(proposalScene.quads.filter(q=>q.texture.endsWith('msgbox_blackbox.png')).map(q=>q.rect),[[494,455,100,24],[606,455,100,24]]);
  assert.deepEqual(await page.locator('[data-ui-id="invite-accept"]').boundingBox(),{x:522,y:501,width:76,height:24});
  assert.equal(await page.locator('[data-ui-id="logout"]').count(),0,'modal removes background input targets');
  await page.screenshot({path:'temp/artifacts/mission-chrome/party-proposal.png'});
  const proposalCapture=(await page.screenshot()).toString('base64');
  const tilePixels=await page.evaluate(async capture=>{
   const load=async url=>createImageBitmap(await(await fetch(url)).blob());
   const [tile,screen]=await Promise.all([load('/assets/images/Media_extracted/interface/ifcommon/bg_tile/com_bg_tile_b.png'),load('data:image/png;base64,'+capture)]);
   const canvas=new OffscreenCanvas(1200,900),ctx=canvas.getContext('2d');ctx.drawImage(screen,0,0);
   const actual=ctx.getImageData(470,444,260,8).data;
   const source=new OffscreenCanvas(128,128),s=source.getContext('2d');s.drawImage(tile,0,0);const expected=s.getImageData(0,0,128,128).data;
   let max=0,total=0;for(let y=0;y<8;y++)for(let x=0;x<260;x++)for(let c=0;c<3;c++){
    const delta=Math.abs(actual[(y*260+x)*4+c]-expected[(((42+y)%128)*128+(4+x)%128)*4+c]);max=Math.max(max,delta);total+=delta;
   }
   tile.close();screen.close();return {max,mean:total/(260*8*3),pixels:260*8};
  },proposalCapture);
  await writeFile('temp/artifacts/mission-chrome/party-pixel-result.json',JSON.stringify(tilePixels,null,2));
  assert.ok(tilePixels.max<=5&&tilePixels.mean<1,JSON.stringify(tilePixels));
  await page.locator('[data-ui-id="invite-accept"]').click();
  assert.deepEqual(await page.evaluate(()=>chromeFixture.commands.at(-1)),{kind:'gameplay',command:{kind:'social-consent',accept:true}});
  await page.evaluate(()=>{chromeFixture.state.gameplay={...chromeFixture.state.gameplay,social:{invitation:null}};chromeFixture.draw();});
  assert.equal(await page.locator('[data-ui-id="invite-accept"]').count(),0);
  await page.evaluate(()=>{chromeFixture.state.gameplay={...chromeFixture.state.gameplay,social:{invitation:{type:1,gid:7}}};chromeFixture.draw();});
  await page.locator('[data-ui-id="invite-refuse"]').click();
  assert.deepEqual(await page.evaluate(()=>chromeFixture.commands.at(-1)),{kind:'gameplay',command:{kind:'social-consent',accept:false}});
  await page.evaluate(close=>{chromeFixture.state.gameplay={...chromeFixture.state.gameplay,social:{invitation:null},pose:{regionId:0x6262,x:900,y:0,z:900,angle:0}};chromeFixture.draw();chromeFixture.ui.event({kind:'activate',id:close});chromeFixture.draw();},panelCloseId('System'));
  await page.waitForFunction(()=>{chromeFixture.draw();return chromeFixture.ui.stats().pending===0&&chromeFixture.scene.quads.some(q=>q.mask);});
  await page.screenshot({path:'temp/artifacts/mission-chrome/hud-1200.png'});
  assert.equal(await page.locator('[data-ui-id="logout"]').count(),0);
  assert.ok(await page.evaluate(()=>chromeFixture.scene.quads.some(q=>q.texture.includes('/minimap/98x98.png')&&q.mask&&q.rect[2]===160)),'published map tile uses native initial zoom');
  await page.locator('[data-ui-id="minimap-in"]').click();await page.evaluate(()=>chromeFixture.draw());
  await page.waitForFunction(()=>{chromeFixture.draw();return chromeFixture.scene.quads.some(q=>q.texture.includes('/minimap/98x98.png')&&q.rect[2]===Math.fround(160+19.200000762939453));});
  await page.evaluate(()=>{chromeFixture.state.width=1024;chromeFixture.state.height=768;chromeFixture.draw();});
  assert.deepEqual(await page.evaluate(()=>chromeFixture.scene.quads.find(q=>q.texture.endsWith('ub_window_01.png')).rect),[112,716,800,52]);
  assert.deepEqual(await page.evaluate(()=>chromeFixture.scene.quads.find(q=>q.texture.endsWith('pmi_hp.png')).rect),[83,32,124,12]);
  await page.evaluate(()=>{chromeFixture.state.width=1200;chromeFixture.state.height=900;chromeFixture.draw();});
  await page.evaluate(async()=>{
   const {createGameplay}=await import('/src/engine/runtime/simulation/worker/session/world/gameplay/gameplay.ts');
   const frames=[],gameplay=createGameplay(f=>frames.push({opcode:f.opcode,payload:[...f.payload]}));gameplay.bootstrap({academyMember:false,eventGuideStateMask:0,localPlayerEntry:{countryByte9c:0}});gameplay.seed({gid:1,regionId:0x6262,x:900,y:0,z:900,heading:0});
   chromeFixture.guideGame=gameplay;chromeFixture.guideFrames=frames;chromeFixture.guideCursor=chromeFixture.commands.length;
   const seed=gameplay.take();chromeFixture.state.gameplay={...chromeFixture.state.gameplay,pose:{regionId:0x62a7,x:900,y:0,z:900,angle:0},guide:seed.guide,academy:seed.academy};
   chromeFixture.guideDraw=()=>{for(const c of chromeFixture.commands.slice(chromeFixture.guideCursor)){if(c.kind==='gameplay'&&(c.command.kind==='guide-event'||c.command.kind.startsWith('academy-')))gameplay.command(c.command,0,undefined);}
    chromeFixture.guideCursor=chromeFixture.commands.length;const s=gameplay.take();if(s)chromeFixture.state.gameplay={...chromeFixture.state.gameplay,guide:s.guide,academy:s.academy};chromeFixture.draw();};
  });
  await page.waitForFunction(()=>{chromeFixture.guideDraw();return chromeFixture.scene.quads.some(q=>q.texture.endsWith('gd_paper.png'))&&chromeFixture.ui.stats().pending===0;},null,{timeout:10000}).catch(async e=>{await page.screenshot({path:'temp/artifacts/mission-chrome/guide-failure.png'});throw Error(e.message+' '+JSON.stringify(await page.evaluate(()=>({frames:chromeFixture.guideFrames,commands:chromeFixture.commands.slice(-5),state:chromeFixture.state.gameplay.guide,stats:chromeFixture.ui.stats(),requests:chromeFixture.requests.slice(-15)}))));});
  assert.deepEqual(await page.evaluate(()=>chromeFixture.guideFrames[0]),{opcode:0x707b,payload:[1,0,0,0]});
  assert.ok(await page.evaluate(()=>chromeFixture.scene.quads.some(q=>q.texture.endsWith('gd_start.png'))),'inline retail image must be published, not silently missing');
  await page.screenshot({path:'temp/artifacts/mission-chrome/first-login-guide.png'});
  const guideCapture=(await page.screenshot()).toString('base64');
  const guidePixels=await page.evaluate(async capture=>{
   const q=chromeFixture.scene.quads.find(q=>q.texture.endsWith('gd_start.png'));
   const load=async p=>createImageBitmap(await(await fetch(p)).blob());const [native,actual]=await Promise.all([load(q.texture),load('data:image/png;base64,'+capture)]);
   const expectedCanvas=new OffscreenCanvas(64,48),actualCanvas=new OffscreenCanvas(64,48),e=expectedCanvas.getContext('2d'),a=actualCanvas.getContext('2d');
   e.drawImage(native,16,16,64,48,0,0,64,48);a.drawImage(actual,q.rect[0]+16,q.rect[1]+16,64,48,0,0,64,48);
   const ep=e.getImageData(0,0,64,48).data,ap=a.getImageData(0,0,64,48).data;let max=0,count=0;for(let i=0;i<ep.length;i+=4)if(ep[i+3]===255){count++;for(let c=0;c<3;c++)max=Math.max(max,Math.abs(ep[i+c]-ap[i+c]));}native.close();actual.close();return {max,count,position:q.rect.slice(0,2)};
  },guideCapture);
  assert.deepEqual(guidePixels.position,[413,270]);assert.ok(guidePixels.count>2000&&guidePixels.max<=2,JSON.stringify(guidePixels));
  await writeFile('temp/artifacts/mission-chrome/guide-pixel-result.json',JSON.stringify(guidePixels,null,2));
  const retained=await page.evaluate(()=>{chromeFixture.holdAssets=true;const before=chromeFixture.scene.quads.filter(q=>q.texture.endsWith('gd_paper.png'));chromeFixture.ui.event({kind:'activate',id:'guide-sidebar'});for(let i=0;i<10;i++)chromeFixture.guideDraw();return {before,after:chromeFixture.scene.quads.filter(q=>q.texture.endsWith('gd_paper.png')),present:!!document.querySelector('[data-ui-id="guide-drag"]')};});
  assert.ok(retained.present);assert.deepEqual(retained.after,retained.before,'cold sidebar assets retain the admitted popup');
  await page.evaluate(()=>{chromeFixture.holdAssets=false;});
  await page.waitForFunction(()=>{chromeFixture.guideDraw();return chromeFixture.ui.stats().pending===0&&document.querySelector('[data-ui-id="guide-tab:general"]');});
  assert.deepEqual(await page.locator('[data-ui-id^="guide-tab:"]').allTextContents(),['Help','Alarm','Quest']);
  for(const tab of ['general','quests','events']){
   await page.locator('[data-ui-id="guide-tab:'+tab+'"]').click();
   await page.waitForFunction(()=>{chromeFixture.guideDraw();if(!document.querySelector('[data-ui-id="guide-drag"]'))throw Error('Guide disappeared');return chromeFixture.ui.stats().pending===0;});
   await page.screenshot({path:'temp/artifacts/mission-chrome/guide-'+tab+'.png'});
  }

  await page.locator(`[data-ui-id="${panelCloseId('Game Guide')}"]`).click();await page.evaluate(()=>chromeFixture.guideDraw());
  await page.evaluate(()=>{for(let i=0;i<20;i++)chromeFixture.guideDraw();});
  assert.equal(await page.evaluate(()=>chromeFixture.guideFrames.filter(f=>f.payload[0]===1).length),1,'closing does not acknowledge or reopen welcome again');
  await page.locator('[data-ui-id="academy-open"]').click();await page.evaluate(()=>chromeFixture.guideDraw());
  assert.deepEqual(await page.evaluate(()=>chromeFixture.guideFrames.at(-1)),{opcode:0x7701,payload:[0]});
  await page.evaluate(()=>chromeFixture.guideGame.receive({opcode:0xb701,payload:Uint8Array.of(1,0,0,0)},0));
  await page.waitForFunction(()=>{chromeFixture.guideDraw();return chromeFixture.ui.stats().pending===0&&document.querySelector('[data-ui-id="academy-refresh"]');},null,{timeout:6000}).catch(async e=>{await page.screenshot({path:'temp/artifacts/mission-chrome/academy-failure.png'});throw Error(e.message+' '+JSON.stringify(await page.evaluate(()=>window.chromeFixture?{stats:chromeFixture.ui.stats(),requests:chromeFixture.requests.slice(-20),commands:chromeFixture.commands.slice(-4)}:'reloaded')));});
  await page.screenshot({path:'temp/artifacts/mission-chrome/academy-matching.png'});
  assert.deepEqual(await page.evaluate(()=>chromeFixture.ui.stats().failed),[],'academy must not request invented chrome textures');
  await page.locator(`[data-ui-id="${panelCloseId('Academy Matching')}"]`).click();await page.evaluate(()=>chromeFixture.guideDraw());
  await page.evaluate(()=>{chromeFixture.ui.event({kind:'activate',id:'open-window:Game Guide'});chromeFixture.guideDraw();});
  await page.waitForFunction(()=>{chromeFixture.guideDraw();return document.querySelector('[data-ui-id="guide-sidebar"]');});
  await page.evaluate(()=>{chromeFixture.ui.event({kind:'drag',id:'guide-drag',dx:200,dy:0});chromeFixture.guideDraw();});
  await page.locator('[data-ui-id="guide-sidebar"]').click();await page.evaluate(()=>chromeFixture.guideDraw());
  await page.locator('[data-ui-id="guide-tab:general"]').click();await page.evaluate(()=>chromeFixture.guideDraw());
  await page.locator('[data-ui-id="guide-group:1000"]').click();await page.evaluate(()=>chromeFixture.guideDraw());
  await page.locator('[data-ui-id="guide-article:1001"]').click();await page.evaluate(()=>chromeFixture.guideDraw());
  await page.screenshot({path:'temp/artifacts/mission-chrome/guide-index.png'});
  await page.locator(`[data-ui-id="${panelCloseId('Game Guide')}"]`).click();await page.evaluate(()=>chromeFixture.guideDraw());
  const europeanContent=await page.evaluate(async()=>{const catalog=await(await fetch('/assets/data/event-guide-catalog.json')).json();return catalog.eventRowsByState[1].englishEuropeanContent;});
  if(europeanContent===''){
   await page.evaluate(()=>{
    chromeFixture.state.session={...chromeFixture.state.session,phase:'signed-out'};chromeFixture.draw();
    chromeFixture.guideGame.bootstrap({eventGuideStateMask:0,localPlayerEntry:{countryByte9c:1}});chromeFixture.guideGame.seed({gid:1,regionId:0x62a7,x:900,y:0,z:900,heading:0});
    chromeFixture.state.session={...chromeFixture.state.session,phase:'world'};chromeFixture.state.gameplay={...chromeFixture.state.gameplay,guide:chromeFixture.guideGame.take().guide};chromeFixture.guideFrames.length=0;
    for(let i=0;i<20;i++)chromeFixture.guideDraw();
   });
   assert.deepEqual(await page.evaluate(()=>chromeFixture.guideFrames),[],'unavailable English article must not be acknowledged as shown');
   assert.equal(await page.evaluate(()=>chromeFixture.scene.quads.some(q=>q.texture.endsWith('gd_paper.png'))),false);
  }
  const maskCheck=await page.evaluate(async()=>{
   const load=async path=>createImageBitmap(await(await fetch(path)).blob());
   const mask=await load('/assets/images/Media_extracted/interface/minimap/mm_alpha.png');
   chromeFixture.renderer.setUiTexture('mask-oracle',mask);
   chromeFixture.renderer.setUiTexture('cutoff-oracle',new ImageData(Uint8ClampedArray.of(255,0,0,127,255,0,0,128),2,1));
   chromeFixture.renderer.setUiTexture('turn-oracle',new ImageData(Uint8ClampedArray.of(255,0,0,255,0,255,0,255,0,0,255,255,255,255,0,255),2,2));
   chromeFixture.renderer.setUi({revision:999,width:1200,height:900,quads:[{rect:[20,20,105,105],texture:'',uv:[0,0,1,1],color:[1,0,0,1],clip:[20,20,105,105],mask:{texture:'mask-oracle',rect:[20,20,105,105]}},{rect:[200,20,32,16],texture:'cutoff-oracle',uv:[0,0,1,1],color:[1,1,1,1],clip:[200,20,32,16],alphaCutoff:128/255},{rect:[300,20,32,32],texture:'turn-oracle',uv:[0,0,1,1],color:[1,1,1,1],clip:[300,20,32,32],uvTurn:1}]});
   chromeFixture.renderer.frame({width:1200,height:900});
   const image=await createImageBitmap(document.querySelector('canvas')),canvas=new OffscreenCanvas(1200,900),ctx=canvas.getContext('2d');ctx.drawImage(image,0,0);image.close();
   return {error:chromeFixture.renderer.error(),phase:chromeFixture.renderer.phase(),corner:[...ctx.getImageData(21,21,1,1).data],center:[...ctx.getImageData(72,72,1,1).data],below:[...ctx.getImageData(203,25,1,1).data],above:[...ctx.getImageData(228,25,1,1).data],rotated:[...ctx.getImageData(303,23,1,1).data]};
  });
  assert.ok(maskCheck.corner[0]<30,'mask removes square corners');assert.ok(maskCheck.center[0]>240&&maskCheck.center[1]<10,'mask retains central map content '+JSON.stringify(maskCheck));
  assert.equal(maskCheck.error,null);assert.ok(maskCheck.below[0]<30&&maskCheck.above[0]>100,'native 128/255 alpha threshold '+JSON.stringify(maskCheck));assert.ok(maskCheck.rotated[2]>240&&maskCheck.rotated[0]<10,'native +90 degree edge rotation '+JSON.stringify(maskCheck));
  await page.evaluate(()=>{chromeFixture.ui.dispose();chromeFixture.platform.dispose();chromeFixture.renderer.dispose();});
 }finally{await browser.close();}
});


test('minimap zoom animates and both Enter keys focus chat through browser input',{timeout:60000},async()=>{
 const {browser,page}=await launchProbeBrowser();
 try{
  await createChromeFixture(page);
  await page.evaluate(()=>{
   chromeFixture.ui.event({kind:'key',code:'Escape'});
   chromeFixture.state.gameplay={...chromeFixture.state.gameplay,pose:{regionId:0x6262,x:900,y:0,z:900,angle:0}};
  });
  await page.waitForFunction(()=>{chromeFixture.draw();return chromeFixture.scene.quads.some(q=>q.texture.endsWith('/minimap/98x98.png'));});
  await page.locator('[data-ui-id="minimap-in"]').click();
  const widths=await page.evaluate(()=>{
   const sample=()=>chromeFixture.scene.quads.find(q=>q.texture.endsWith('/minimap/98x98.png')).rect[2];
   const now=performance.now();
   const result=[sample()];for(const dt of [10,20,100,200,400]){chromeFixture.ui.step(chromeFixture.state,now+dt);result.push(sample());}
   return result;
  });
  assert.equal(widths[0],160);assert.ok(widths[1]>160&&widths[1]<179.2);
  for(let i=1;i<widths.length;i++)assert.ok(widths[i]>widths[i-1]);
  assert.equal(widths.at(-1),Math.fround(179.20000076293945));
  for(const key of ['Enter','NumpadEnter']){
   await page.evaluate(()=>{document.activeElement?.blur();chromeFixture.ui.event({kind:'focus',id:null});chromeFixture.draw();});
   await page.keyboard.press(key);
   await page.waitForFunction(()=>{chromeFixture.draw();return document.activeElement?.dataset.uiId==='chat-text';});
  }
  await mkdir('temp/artifacts/mission-chrome',{recursive:true});
  await page.screenshot({path:'temp/artifacts/mission-chrome/minimap-chat-fixed.png'});
  await writeFile('temp/artifacts/mission-chrome/minimap-chat-fixed.json',JSON.stringify({widths,keys:['Enter','NumpadEnter'],passed:true},null,2));
 }finally{await browser.close();}
});


test('region crossing presents complete native banner and does not replay inside the same named zone',{timeout:60000},async()=>{
 const {browser,page}=await launchProbeBrowser();try{
  await createChromeFixture(page);
  await page.evaluate(()=>{chromeFixture.ui.event({kind:'key',code:'Escape'});chromeFixture.state.gameplay.pose={regionId:0,x:900,y:0,z:900,angle:0};});
  await page.waitForFunction(()=>{chromeFixture.draw();return chromeFixture.requests.some(p=>p.endsWith('/area_deco.png'))&&chromeFixture.ui.stats().pending===0;});
  const result=await page.evaluate(async()=>{
   const zones=(await(await fetch('/assets/text/textzonename.en.json')).json()).entries;
   const codes=(await(await fetch('/assets/text/regioncode.json')).json()).entries;
   const key=Object.keys(zones).find(k=>k.endsWith('_01')&&zones[k]==='China Grassland').slice(0,-3);
   const regions=Object.keys(codes).filter(k=>codes[k]===key).map(Number);if(regions.length<2)throw Error('Need two sectors sharing grassland');
   const base=performance.now();
   const at=(region,time)=>{chromeFixture.state.gameplay={...chromeFixture.state.gameplay,pose:{...chromeFixture.state.gameplay.pose,regionId:region}};chromeFixture.ui.step(chromeFixture.state,base+time);return chromeFixture.scene.quads.find(q=>q.texture.endsWith('/area_deco.png'))??null;};
   const initial=at(regions[0],0),fading=at(regions[0],250),full=at(regions[0],500),same=at(regions[1],2000),gone=at(regions[1],3500);
   at(0,3600);at(regions[0],3700);const reenter=at(regions[0],4200);
   chromeFixture.renderer.frame({width:1200,height:900});
   return {initial,fading,full,same,gone,reenter,title:zones[key+'_01'],description:zones[key+'_02'],regions:regions.slice(0,2)};
  });
  assert.equal(result.initial,null);assert.ok(result.fading.color[3]>0&&result.fading.color[3]<1);
  assert.deepEqual(result.full.rect,[228,95,744,104]);assert.equal(result.full.color[3],1);
  assert.equal(result.same.color[3],1);assert.equal(result.gone,null);assert.equal(result.reenter.color[3],1);
  await mkdir('temp/artifacts/mission-chrome',{recursive:true});
  await page.screenshot({path:'temp/artifacts/mission-chrome/region-banner-fixed.png'});
  await writeFile('temp/artifacts/mission-chrome/region-banner-fixed.json',JSON.stringify(result,null,2));
 }finally{await browser.close();}
});
