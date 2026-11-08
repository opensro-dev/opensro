import {test} from 'node:test';
import assert from 'node:assert/strict';
import {mkdir} from 'node:fs/promises';
import {launchProbeBrowser} from '../../../../scripts/lib/probeBrowser.mjs';
import {CLIENT_NEXT_BASE_URL} from '../../../../scripts/lib/probeEndpoints.mjs';
import {holdProbeRuntime} from './helpers/hold-runtime.mjs';

test('GPU retail quickslot bars support native layouts, real skill dragging, tooltip and synchronized cooldowns',{timeout:60000},async()=>{
 const {browser,page}=await launchProbeBrowser();const errors=[];page.on('pageerror',e=>errors.push(e.message));
 try{
  await holdProbeRuntime(page);await page.setViewportSize({width:1600,height:900});await page.goto(CLIENT_NEXT_BASE_URL);
  await page.waitForFunction(()=>document.querySelector('output')?.textContent?.includes('runtime: running'));
  await page.evaluate(async()=>{
   const {runtime}=await import(Array.from(document.scripts).find(s=>s.src&&new URL(s.src).pathname==='/src/bootstrap.ts').src);runtime.dispose();
   const {createUi}=await import('/src/engine/runtime/ui/ui.ts'),{createAssets}=await import('/src/engine/runtime/assets/assets.ts'),{createRenderer}=await import('/src/engine/runtime/renderer/renderer.ts'),{createPlatform}=await import('/src/engine/runtime/platform/platform.ts');
   const canvas=document.querySelector('canvas'),renderer=createRenderer(canvas),assets=createAssets(),commands=[];let scene=null,semantics=null;
   const skill={id:3,group:174,level:1,name:'SKILL_CH_SWORD_SMASH_A',nameSymbol:'SN_SKILL_CH_SWORD_SMASH_A',icon:'skill/china/sword_smash_a.ddj',spCost:1,reqStr:0,reqInt:0,trainable:true,targetRequired:true,cooldownMs:5000,cooldownGroup:0,masteries:[{ID:257,Level:1},{ID:0,Level:0}],prerequisites:[{ID:0,Level:0},{ID:0,Level:0},{ID:0,Level:0}]};
   const state={session:{phase:'world',revision:1,character:'Fixture'},gameplay:{localGid:1,skills:[3],skillCatalog:[skill],quickSlots:[{slot:1,kind:0x49,payload:3}],inventory:[],vitals:[],casts:[],target:8,progression:{level:10,skillPoints:0,masteries:[{id:257,level:1}]}},entities:[{gid:1,refObjId:1907,name:'Fixture',kind:'player',regionId:1,x:0,y:0,z:0,heading:0},{gid:8,name:'Target',kind:'monster',regionId:1,x:2,y:0,z:0,heading:0}],width:1600,height:900,worldReady:true,simulationTimeMs:1000};
   const ui=createUi({assets,commands:c=>{commands.push(c);if(c.kind==='gameplay'&&c.command.kind==='quickslot-set'){const binding=c.command.binding;state.gameplay={...state.gameplay,quickSlots:[...state.gameplay.quickSlots.filter(r=>r.slot!==binding.slot),binding]};}},publish:s=>{scene=s;renderer.setUi(s)},texture:renderer.setUiTexture,base:location.origin,apiBase:'http://fixture.invalid',saveQuickslots:value=>platform.saveQuickslotOptions(value)});
   const platform=createPlatform(canvas,document.querySelector('output'),()=>{},()=>{},()=>{},ui.event,ui.blocks);
   ui.event({kind:'quickslot-preferences',value:{open:true,vertical:true,double:true,transparent:false,slotLock:false,positionLock:false,position:null}});
   const draw=()=>{const result=ui.step({...state},performance.now());if(result){semantics=result;platform.presentUi(result);}renderer.frame({width:1600,height:900});};
   const settle=async()=>{const end=performance.now()+20000;let stable=0;while(performance.now()<end){draw();if(ui.stats().pending===0&&!ui.stats().failed.length&&ui.stats().windowReady&&semantics?.controls.some(c=>c.id==='hotbar:41'||c.id==='skill-confirm-ok')){if(++stable>=4)return;}else stable=0;await new Promise(requestAnimationFrame);}throw Error('Quickslot HUD did not settle: '+JSON.stringify(ui.stats()));};
   window.quickbarFixture={ui,platform,renderer,assets,state,commands,draw,settle,scene:()=>scene,semantics:()=>semantics};await settle();
  });
  const rows=await page.evaluate(()=>quickbarFixture.semantics().controls.filter(c=>c.id.startsWith('hotbar:')).map(c=>c.id));assert.equal(rows.length,21);assert.ok(rows.includes('hotbar:50'));
  await page.evaluate(()=>{quickbarFixture.ui.event({kind:'activate',id:'open-window:Skills'});return quickbarFixture.settle();});
  await page.evaluate(()=>{const f=quickbarFixture;f.state.gameplay={...f.state.gameplay,skills:[],progression:{...f.state.gameplay.progression,skillPoints:1}};return f.settle();});
  assert.equal(await page.evaluate(()=>quickbarFixture.semantics().controls.find(c=>c.id==='skill:3').draggable),false);
  await page.locator('[data-ui-id="skill-learn:3"]').click();await page.evaluate(()=>quickbarFixture.settle());
  const practice=await page.evaluate(()=>{
   const quads=quickbarFixture.scene().quads,x=640,y=283;
   return {
    fill:quads.find(q=>q.texture===''&&q.rect.join() === [x+20,y+101,280,112].join())?.color,
    masteryDecoration:quads.some(q=>q.texture.endsWith('/npc_mastery_namebox.png')),
    caption:quads.filter(q=>q.texture.startsWith('/assets/fonts/')&&q.rect[0]>=x+10&&q.rect[0]<x+309&&q.rect[1]>=y+11&&q.rect[1]<y+30).length,
   };
  });
  assert.deepEqual(practice.fill,[15/255,15/255,15/255,1],'native skill description is opaque');
  assert.equal(practice.masteryDecoration,false,'skill mode hides mastery-only decoration');
  assert.ok(practice.caption>0,'native skill practice caption is localized and drawn');
  const practicePixel=await page.evaluate(async()=>{
   quickbarFixture.draw();
   const bitmap=await createImageBitmap(document.querySelector('canvas')),sample=new OffscreenCanvas(1,1),context=sample.getContext('2d');
   context.drawImage(bitmap,665,490,1,1,0,0,1,1);bitmap.close();return Array.from(context.getImageData(0,0,1,1).data);
  });assert.deepEqual(practicePixel,[15,15,15,255],'GPU output preserves the native opaque description fill');
  await mkdir('temp/artifacts/quickslots',{recursive:true});await page.screenshot({path:'temp/artifacts/quickslots/skill-practice.png'});
  await page.locator('[data-ui-id="skill-confirm-cancel"]').click();await page.evaluate(()=>quickbarFixture.settle());
  assert.equal(await page.evaluate(()=>quickbarFixture.commands.filter(c=>c.kind==='gameplay'&&c.command.kind==='skill-train').length),0,'cancel cannot send training');
  await page.locator('[data-ui-id="skill-learn:3"]').click();await page.evaluate(()=>quickbarFixture.settle());
  await page.locator('[data-ui-id="skill-confirm-ok"]').click();await page.evaluate(()=>quickbarFixture.settle());
  assert.deepEqual(await page.evaluate(()=>quickbarFixture.commands.at(-1)),{kind:'gameplay',command:{kind:'skill-train',id:3}});
  assert.equal(await page.evaluate(()=>quickbarFixture.semantics().controls.find(c=>c.id==='skill:3').draggable),false,'request alone cannot grant a skill');
  await page.evaluate(()=>{const f=quickbarFixture;f.state.gameplay={...f.state.gameplay,skills:[3],progression:{...f.state.gameplay.progression,skillPoints:0}};return f.settle();});
  await page.locator('[data-ui-id="skill:3"]').dragTo(page.locator('[data-ui-id="hotbar:41"]'));
  await page.evaluate(()=>quickbarFixture.settle());
  assert.deepEqual(await page.evaluate(()=>quickbarFixture.state.gameplay.quickSlots.find(r=>r.slot===41)),{slot:41,kind:0x49,payload:3});
  await page.evaluate(()=>{quickbarFixture.ui.event({kind:'key',code:'Escape'});quickbarFixture.draw();});
  await page.locator('[data-ui-id="hotbar:41"]').click();await page.evaluate(()=>quickbarFixture.draw());
  assert.deepEqual(await page.evaluate(()=>quickbarFixture.commands.at(-1)),{kind:'gameplay',command:{kind:'skill',skillId:3,gid:8}});
  await page.locator('[data-ui-id="hotbar:41"]').hover();await page.evaluate(()=>quickbarFixture.settle());
  const tooltip=await page.evaluate(()=>({paths:quickbarFixture.scene().quads.map(q=>q.texture),count:quickbarFixture.scene().quads.length}));assert.ok(tooltip.count>100);
  await mkdir('temp/artifacts/quickslots',{recursive:true});await page.screenshot({path:'temp/artifacts/quickslots/vertical-tooltip.png'});
  await page.evaluate(()=>{const f=quickbarFixture;f.ui.event({kind:'quickslot-preferences',value:{open:true,vertical:true,double:true,transparent:true,slotLock:false,positionLock:false,position:null}});return f.settle();});
  const alpha=await page.evaluate(()=>{
   const f=quickbarFixture,quads=f.scene().quads,slot=f.semantics().controls.find(c=>c.id==='hotbar:41').rect;
   return {icon:quads.find(q=>q.texture.endsWith('/sword_smash_a.png')&&q.rect[0]===slot[0]&&q.rect[1]===slot[1])?.color[3],border:quads.filter(q=>/com_tooltip_(corner|edge)\.png$/.test(q.texture)).map(q=>q.color[3])};
  });assert.equal(alpha.icon,110/255,'548700 transparency still applies to a hovered shortcut icon');assert.deepEqual(alpha.border,Array(8).fill(196/255),'6791C0 live border alpha is independent of hotbar transparency');
  await page.screenshot({path:'temp/artifacts/quickslots/transparent-tooltip.png'});
  await page.evaluate(()=>{const f=quickbarFixture;f.ui.event({kind:'hover',id:null});f.state.gameplay={...f.state.gameplay,skillCooldowns:[{skill:3,group:0,startedAtMs:1000,durationMs:5000}]};f.state.simulationTimeMs=3500;return f.settle();});
  const cooldown=await page.evaluate(()=>quickbarFixture.scene().quads.filter(q=>q.texture.endsWith('/skill_delay.png')).map(q=>({rect:q.rect,uv:q.uv})));assert.equal(cooldown.length,2,JSON.stringify(await page.evaluate(()=>({ui:quickbarFixture.ui.stats(),health:quickbarFixture.assets.health(),rows:quickbarFixture.state.gameplay.skillCooldowns,now:quickbarFixture.state.simulationTimeMs,paths:quickbarFixture.scene().quads.filter(q=>q.texture.includes('delay')).map(q=>q.texture)}))));assert.deepEqual(cooldown[0].uv,cooldown[1].uv);
  assert.deepEqual(await page.evaluate(()=>quickbarFixture.scene().quads.filter(q=>q.texture.endsWith('/skill_delay.png')).map(q=>q.color[3])),[1,1],'53FD40 resets alpha before cooldown rendering, including the transparent extended bar');
  await page.screenshot({path:'temp/artifacts/quickslots/cooldown.png'});
  const before=await page.evaluate(()=>quickbarFixture.commands.length);await page.locator('[data-ui-id="hotbar:41"]').click();assert.equal(await page.evaluate(()=>quickbarFixture.commands.length),before);
  for(const [name,vertical,double]of [['horizontal-one',false,false],['horizontal-two',false,true],['vertical-one',true,false],['vertical-two',true,true]]){
   await page.evaluate(async({vertical,double})=>{const f=quickbarFixture;f.ui.event({kind:'quickslot-preferences',value:{open:true,vertical,double,transparent:false,slotLock:false,positionLock:false,position:null}});await f.settle();},{vertical,double});
   await page.screenshot({path:'temp/artifacts/quickslots/'+name+'.png'});
  }
  await page.locator('[data-ui-id="ext-options"]').click();await page.evaluate(()=>quickbarFixture.settle());
  await page.screenshot({path:'temp/artifacts/quickslots/options.png'});
  await page.locator('[data-ui-id="ext-double"]').click();await page.evaluate(()=>quickbarFixture.draw());
  await page.locator('[data-ui-id="ext-options-apply"]').click();await page.evaluate(()=>quickbarFixture.settle());
  assert.equal(await page.evaluate(()=>JSON.parse(localStorage.getItem('sro:v1150:extended-quickslot:1')).double),false);
  assert.ok(await page.locator('[data-ui-id="ext-options-ok"]').isVisible(),'Apply keeps the retail options window open');
  await page.locator('[data-ui-id="ext-options-ok"]').click();await page.evaluate(()=>quickbarFixture.draw());
  await page.evaluate(()=>{quickbarFixture.state.simulationTimeMs=6500;return quickbarFixture.settle();});assert.equal(await page.evaluate(()=>quickbarFixture.scene().quads.filter(q=>q.texture.endsWith('/skill_delay.png')||q.texture.endsWith('/skill_charge.png')).length),0);
  const pixels=await page.evaluate(async()=>{
   const f=quickbarFixture,{quickslotCooldownQuads}=await import('/src/engine/foundation/ui/quickslot-cooldown.ts'),clip=[0,0,1600,900],r=[0,0,32,32];
   const atlas=quickslotCooldownQuads([{skill:3,group:0,startedAtMs:1000,durationMs:5000}],3,0,3500,r,clip)[0];
   f.renderer.setUi({revision:999999,width:1600,height:900,quads:[{rect:r,clip,uv:[0,0,1,1],texture:'',color:[40/255,80/255,120/255,1]},atlas]});f.renderer.frame({width:1600,height:900});
   const image=await createImageBitmap(document.querySelector('canvas')),actual=new OffscreenCanvas(32,32),a=actual.getContext('2d');a.drawImage(image,0,0);image.close();
   const bitmap=await createImageBitmap(await (await fetch('/assets/images/Media_extracted/interface/skill/skill_delay.png')).blob()),reference=new OffscreenCanvas(32,32),b=reference.getContext('2d');b.fillStyle='rgb(40,80,120)';b.fillRect(0,0,32,32);
   // Frozen native half-time selector: trunc(239 - .5*239) = 119,
   // column 7, row 7. The shipped 256px atlas is magnified into a 32px slot.
   b.drawImage(bitmap,112,112,16,16,0,0,32,32);bitmap.close();const actualBytes=a.getImageData(0,0,32,32).data,expected=b.getImageData(0,0,32,32).data;let max=0,differing=0;
   for(let y=1;y<31;y++)for(let x=1;x<31;x++){let difference=0;for(let c=0;c<3;c++)difference=Math.max(difference,Math.abs(actualBytes[(y*32+x)*4+c]-expected[(y*32+x)*4+c]));max=Math.max(max,difference);if(difference>2)differing++;}
   return {pixels:900,maxDifference:max,overTolerance:differing};
  });assert.equal(pixels.overTolerance,0,JSON.stringify(pixels));
  assert.deepEqual(errors,[]);
 }finally{await page.evaluate(()=>{const f=window.quickbarFixture;if(f){f.ui.dispose();f.platform.dispose();f.assets.dispose();f.renderer.dispose();}}).catch(()=>{});await browser.close();}
});
