import {test} from 'node:test';
import assert from 'node:assert/strict';
import {mkdir,writeFile} from 'node:fs/promises';
import {launchProbeBrowser} from '../../../../scripts/lib/probeBrowser.mjs';
import {bootPlayableSession} from './helpers/playable-session.mjs';
test('authenticated minimap and production GPU marker passes use retail assets',{timeout:180000},async()=>{
 const dir='apps/client-next/temp/artifacts/minimap-coverage';await mkdir(dir,{recursive:true});
 const {browser,page}=await launchProbeBrowser();const errors=[];let tracing=false;
 page.on('pageerror',e=>errors.push(e.message));
 try{
  await bootPlayableSession(page,'asd2');await page.setViewportSize({width:1600,height:900});
  await page.context().tracing.start({screenshots:true,snapshots:true});tracing=true;
  await page.screenshot({path:dir+'/live-world.png'});
  const live=await page.evaluate(()=>({pose:__playableRuntime.gameplay().pose,phase:__playableRuntime.sessionState()?.phase}));
  // Controlled actors exercise the real UI/assets/GPU after authenticated boot.
  // No synthetic entities or movement are sent to the live server.
  await page.evaluate(async()=>{
   const live=__playableRuntime.gameplay();__playableRuntime.dispose();
   const {createUi}=await import('/src/engine/runtime/ui/ui.ts'),{createAssets}=await import('/src/engine/runtime/assets/assets.ts'),{createRenderer}=await import('/src/engine/runtime/renderer/renderer.ts');
   const canvas=document.querySelector('canvas'),renderer=createRenderer(canvas),assets=createAssets();let scene=null;
   const state={width:1600,height:900,worldReady:true,frontend:null,session:{phase:'world',revision:1,character:'asd2'},gameplay:{...live,target:0,pose:{regionId:25256,x:960,y:20,z:960,angle:0},social:{...live.social,self:1,localName:'asd2',members:[]},quests:[],academy:undefined,huntingPoints:[]},entities:[]};
   const ui=createUi({assets,commands:()=>{},publish:s=>{scene=structuredClone(s);renderer.setUi(s);},texture:(id,image)=>renderer.setUiTexture(id,image),base:location.origin,apiBase:location.origin});
   window.__minimap={state,ui,assets,renderer,scene:()=>scene,async settle(){let stable=0;const end=performance.now()+20000;while(performance.now()<end){ui.step({...state},performance.now());renderer.frame({width:1600,height:900});if(ui.stats().error)throw Error(ui.stats().error);if(ui.stats().pending===0&&scene?.quads.some(q=>q.texture.endsWith('mm_sign_character.png'))){if(++stable===8)return;}else stable=0;await new Promise(requestAnimationFrame);}throw Error('Minimap resource deadline '+JSON.stringify(ui.stats()));}};
   const p=state.gameplay.pose;
   state.entities=Array.from({length:520},(_,i)=>({...p,gid:100+i,heading:0,kind:'monster',rarity:i===519?3:0,x:p.x+(i===519?180:-180),z:p.z}));
   state.entities.push({...p,gid:900,heading:0,kind:'npc',x:p.x,z:p.z+180},{...p,gid:901,heading:0,kind:'cos',x:p.x,z:p.z-180},{...p,gid:902,heading:0,kind:'player',x:p.x+120,z:p.z+120});
   state.gameplay.social.members=[{id:2,name:'Party peer',war:65537,region:p.regionId,x:p.x+2000,y:p.y,z:p.z}];
   state.gameplay.academy={member:true,members:[{id:3,name:'Student',war:65537,offline:false,...p,x:p.x-2000}]};
   state.gameplay.huntingPoints=[{...p,gid:950,token:5,name:'Hunt',x:p.x,z:p.z-2000}];await __minimap.settle();
  });
  const markers=await page.evaluate(()=>__minimap.scene().quads.filter(q=>/mm_sign_|wmap_sign_huntingpoint/.test(q.texture)).map(q=>({texture:q.texture,rect:q.rect,rotation:q.rotation,mask:q.mask})));
  for(const name of ['unique','monster','npc','animal','otherplayer','partyarrow','apprenticeshiparrow','character'])assert.ok(markers.some(m=>m.texture.endsWith('mm_sign_'+name+'.png')),name);
  assert.ok(markers.some(m=>m.texture.endsWith('wmap_sign_huntingpoint.png')));assert.equal(markers.filter(m=>m.texture.endsWith('_monster.png')).length,519);
  assert.ok(markers.every(m=>m.mask));await page.screenshot({path:dir+'/marker-branches.png'});
  await page.evaluate(async()=>{const f=__minimap,table=await(await fetch('/assets/data/npcpos.json')).json(),row=table.rows.map(r=>r.split('\t').map(Number)).find(r=>r[1]===f.state.gameplay.pose.regionId);if(!row)throw Error('Missing city quest fixture');f.state.gameplay={...f.state.gameplay,quests:[{refId:3,u08:0x11,u09:0,flags:0,contents:[],targetIds:[row[0]]}]};await f.settle();f.ui.event({kind:'activate',id:'quest-track:3'});await f.settle();});
  const quest=await page.evaluate(async()=>{const end=performance.now()+25000;while(performance.now()<end){await __minimap.settle();const markers=__minimap.scene().quads.filter(q=>/mm_sign_quest/.test(q.texture)).map(q=>({texture:q.texture,rect:q.rect}));if(markers.length)return markers;}throw Error('Quest marker absent '+JSON.stringify({stats:__minimap.ui.stats(),quests:__minimap.state.gameplay.quests}));});assert.equal(quest.length,1);await page.screenshot({path:dir+'/quest-tracking.png'});
  await page.evaluate(async()=>{__minimap.ui.event({kind:'activate',id:'quest-track:3'});await __minimap.settle();});assert.equal(await page.evaluate(()=>__minimap.scene().quads.filter(q=>/mm_sign_quest/.test(q.texture)).length),0);
  await page.evaluate(async()=>{const f=__minimap;f.state.gameplay={...f.state.gameplay,pose:{regionId:32769,x:-10,y:10,z:-10,angle:0},navigationFloor:1,social:undefined,academy:undefined,huntingPoints:[]};f.state.entities=[];await f.settle();});
  await page.waitForFunction(async()=>{await __minimap.settle();return __minimap.scene().quads.some(q=>q.texture.includes('/minimap_d/donwhang/dh_a01_floor02_'));},null,{timeout:25000});
  const dungeon=await page.evaluate(()=>__minimap.scene().quads.filter(q=>q.texture.includes('/minimap_d/')||q.texture.endsWith('mm_dungeonfloor.png')).map(q=>({texture:q.texture,rect:q.rect})));
  assert.ok(dungeon.some(q=>q.texture.endsWith('mm_dungeonfloor.png')));assert.ok(dungeon.filter(q=>q.texture.includes('/minimap_d/')).every(q=>q.texture.includes('floor02')));await page.screenshot({path:dir+'/dungeon-floor.png'});
  await page.evaluate(async()=>{const f=__minimap;f.state.gameplay={...f.state.gameplay,pose:{regionId:(110<<8)|77,x:960,y:0,z:960,angle:0},navigationFloor:undefined,quests:[]};await f.settle();});
  const edge=await page.evaluate(()=>({tiles:__minimap.scene().quads.filter(q=>q.texture.includes('/minimap/')).map(q=>q.texture),stats:__minimap.ui.stats()}));
  assert.ok(edge.tiles.some(p=>p.endsWith('/77x110.png')));assert.ok(!edge.tiles.some(p=>p.endsWith('/77x111.png')));assert.equal(edge.stats.error,null);
  await page.screenshot({path:dir+'/retail-art-edge.png'});
  // Exercise the visible diagnostic with an explicit upstream failure, separate from absent retail art.
  const detail='Asset request failed: /assets/images/Media_extracted/minimap/77x110.png: HTTP 503';
  await page.evaluate(async detail=>{const f=__minimap;f.state.resourceError=detail;for(let i=0;i<30;i++){const semantics=f.ui.step({...f.state},performance.now());if(semantics)f.errorSemantics=semantics;f.renderer.frame({width:1600,height:900});await new Promise(requestAnimationFrame);}},detail);
  assert.equal(await page.evaluate(()=>__minimap.errorSemantics.loadingError),detail);
  await page.screenshot({path:dir+'/visible-resource-error.png'});
  await page.evaluate(async()=>{delete __minimap.state.resourceError;await __minimap.settle();});
  assert.deepEqual(errors,[]);await writeFile(dir+'/incident.json',JSON.stringify({verdict:'PASS SUCCESS',live,markers,quest,dungeon,edge,detail,errors},null,2));
 }catch(error){await page.screenshot({path:dir+'/failure.png'});await writeFile(dir+'/failure.json',JSON.stringify({error:String(error),errors,state:await page.evaluate(()=>({session:window.__playableRuntime?.sessionState(),output:document.querySelector('output')?.textContent}))},null,2));throw error;}
 finally{try{if(tracing)await page.context().tracing.stop({path:dir+'/trace.zip'});}finally{await browser.close();}}
});
