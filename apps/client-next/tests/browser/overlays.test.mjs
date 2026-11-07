import {test} from 'node:test';
import assert from 'node:assert/strict';
import {mkdir,writeFile} from 'node:fs/promises';
import {launchProbeBrowser} from '../../../../scripts/lib/probeBrowser.mjs';
import {CLIENT_NEXT_BASE_URL} from '../../../../scripts/lib/probeEndpoints.mjs';
test('party overlays use shipped art and update through the production UI',{timeout:90000},async()=>{
 const {browser,page}=await launchProbeBrowser();
 try{
 await page.setViewportSize({width:1200,height:900});await page.goto(CLIENT_NEXT_BASE_URL);
 await page.waitForFunction(()=>document.querySelector('output')?.textContent?.includes('runtime: running'));
  await page.evaluate(async()=>{
   localStorage.removeItem('sro:v1150:game-options:1');
   const entry=Array.from(document.scripts).find(s=>s.src&&new URL(s.src).pathname==='/src/bootstrap.ts');
   const {runtime}=await import(entry.src);runtime.dispose();
   const {createUi}=await import('/src/engine/runtime/ui/ui.ts'),{createRenderer}=await import('/src/engine/runtime/renderer/renderer.ts'),{createPlatform}=await import('/src/engine/runtime/platform/platform.ts');
   const jobs=new Map(),requests=[],commands=[],renderer=createRenderer(document.querySelector('canvas'));let serial=0,scene=null;
   const assets={available:()=>Math.max(0,32-jobs.size),request(url,limit,kind){const id=++serial;jobs.set(id,null);requests.push(url);fetch(url).then(async r=>{if(!r.ok)throw Error(r.status+' '+url);const result=kind==='png'?{kind:'image',image:await createImageBitmap(await r.blob())}:{kind:'bytes',buffer:await r.arrayBuffer()};if(jobs.has(id))jobs.set(id,result);else if(result.kind==='image')result.image.close();}).catch(e=>{if(jobs.has(id))jobs.set(id,{kind:'error',error:String(e)});});return id;},take(id){const result=jobs.get(id);if(result)jobs.delete(id);return result;},cancel(id){const result=jobs.get(id);if(result?.kind==='image')result.image.close();jobs.delete(id);}};
   let platform;const ui=createUi({assets,commands:c=>commands.push(c),publish:s=>{scene=s;renderer.setUi(s);},texture:(id,image)=>renderer.setUiTexture(id,image),base:location.origin,apiBase:'http://fixture.invalid',clickSound:()=>{},sound:()=>{},loadingVariant:()=>1,chooseTip:()=>0,saveOptions:value=>platform.saveGameOptions(value)});
   platform=createPlatform(document.querySelector('canvas'),document.querySelector('output'),()=>{},()=>{},()=>{},ui.event,ui.blocks);
   const state={session:{phase:'world',revision:1,character:'Fixture',characters:[{id:1,name:'Fixture',level:1,maxHp:200,maxMp:200}]},gameplay:{localGid:1,inventory:[],inventorySlotCount:45,equipmentSlotCount:13,vitals:[{gid:1,hp:200,mp:200}],casts:[]},entities:[],width:1200,height:900,worldReady:true};
   window.flagFixture={ui,renderer,platform,state,requests,commands,get scene(){return scene;},get pending(){return jobs.size;},draw(){const semantic=ui.step({...state},performance.now());if(semantic)platform.presentUi(semantic);renderer.frame({width:state.width,height:state.height});}};
   const local={gid:1,kind:'local-player',name:'Fixture',regionId:257,x:0,y:0,z:0};
   const peer={...local,gid:2,kind:'player',name:'Peer',guildName:'Guild',guildGrantName:'Officer',guildId:10};
   state.entities=[local,peer];state.gameplay.social={localName:'Fixture',self:11,leader:12,options:0,members:[{id:11,name:'Fixture',model:1907,status:170},{id:12,name:'Peer',model:1907,status:133}],guild:{id:10,name:'Guild',members:[{name:'Fixture',grant:'Master'}]}};
   state.gameplay.attachedEffects=[{gid:2,skill:3,token:123,phase:2}];state.gameplay.skillCatalog=[{id:3,name:'Skill',icon:'skill/china/sword_smash_a.ddj',buffSecondary:false}];
   state.gameplay.vitals.push({gid:2,hp:50,mp:80,abnormal:16});flagFixture.draw();
  });

 await page.waitForFunction(()=>{flagFixture.draw();return flagFixture.pending===0&&document.querySelector('[data-ui-id="party-target:2"]');},null,{timeout:20000});
 await mkdir('temp/artifacts/overlays',{recursive:true});
 await page.screenshot({path:'temp/artifacts/overlays/party.png'});
 const result=await page.evaluate(()=>{
  const f=flagFixture,paths=f.scene.quads.map(q=>q.texture),anchored=f.scene.quads.filter(q=>q.characterAnchor===2);
  const before={paths,anchored:anchored.map(q=>({texture:q.texture,rect:q.rect})),failed:f.ui.stats().failed,rendererError:f.renderer.error()};
  f.ui.event({kind:'activate',id:'party-target:2'});before.command=f.commands.at(-1);
  f.state.entities=f.state.entities.filter(e=>e.gid!==2);f.state.gameplay.revision=2;f.draw();
  before.afterDespawn=f.scene.quads.filter(q=>q.texture.includes('s_poisoning_icon')||q.texture.includes('sword_smash_a')).length;
  f.state.gameplay.social={...f.state.gameplay.social,members:[],leader:0};f.state.gameplay.revision=3;f.draw();
  before.afterLeave=!!document.querySelector('[data-ui-id="party-target:2"]');return before;
 });
 assert.deepEqual(result.failed,[]);assert.equal(result.rendererError,null);
 assert.ok(result.paths.some(p=>p.endsWith('/qpt_hp.png')));assert.ok(result.paths.some(p=>p.endsWith('/s_poisoning_icon.png')));assert.ok(result.paths.some(p=>p.endsWith('/sword_smash_a.png')));
 assert.equal(result.command.command.kind,'select');assert.equal(result.command.command.gid,2);assert.equal(result.afterDespawn,0);assert.equal(result.afterLeave,false);
 assert.ok(result.anchored.some(q=>q.rect[1]<-25));
 await writeFile('temp/artifacts/overlays/browser.json',JSON.stringify(result,null,2));
 await page.evaluate(()=>{flagFixture.ui.dispose();flagFixture.platform.dispose();flagFixture.renderer.dispose();});
 }finally{await browser.close();}
});
