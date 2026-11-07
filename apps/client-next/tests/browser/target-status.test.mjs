import {test} from 'node:test';import assert from 'node:assert/strict';import {mkdir,writeFile} from 'node:fs/promises';
import {launchProbeBrowser} from '../../../../scripts/lib/probeBrowser.mjs';import {CLIENT_NEXT_BASE_URL} from '../../../../scripts/lib/probeEndpoints.mjs';import {holdProbeRuntime} from './helpers/hold-runtime.mjs';
test('target panel uses retail atlas pixels and replaces monster, NPC and player layouts',{timeout:90000},async()=>{
 const {browser,page}=await launchProbeBrowser({viewport:{width:1024,height:768}});try{
 await holdProbeRuntime(page);await page.goto(CLIENT_NEXT_BASE_URL);
 const result=await page.evaluate(async()=>{
 const {createAssets}=await import('/src/engine/runtime/assets/assets.ts'),{createRenderer}=await import('/src/engine/runtime/renderer/renderer.ts'),{createUi}=await import('/src/engine/runtime/ui/ui.ts');
 const canvas=document.createElement('canvas');document.body.append(canvas);const renderer=createRenderer(canvas),assets=createAssets();let scene,semantics;
 const ui=createUi({assets,commands:()=>{},publish:value=>{scene=value;renderer.setUi(value);},texture:(id,image)=>renderer.setUiTexture(id,image),base:location.origin,apiBase:'http://fixture.invalid'});
 const entity={gid:2,refObjId:1933,kind:'monster',name:'Mangyang',level:1,maxHp:100,rarity:0,regionId:25256,x:10,y:0,z:10,heading:0};
 const state={session:{phase:'world',revision:1,character:'Explorer'},gameplay:{localGid:1,target:2,pose:{regionId:25256,x:0,y:0,z:0,angle:0},inventory:[],vitals:[{gid:2,hp:50}],casts:[],progression:{level:1}},entities:[entity],width:1024,height:768,worldReady:true};
 async function settle(){const end=performance.now()+25000;let stable=0;while(performance.now()<end){semantics=ui.step({...state},performance.now())??semantics;renderer.frame({width:1024,height:768});if(renderer.error())throw Error(renderer.error());if(ui.stats().pending===0&&scene?.quads.some(q=>q.texture.endsWith('tw_hp.png'))){if(++stable>4)return;}else if(ui.stats().pending===0&&entity.kind==='player'&&semantics?.controls.some(c=>c.id==='clear-target')){if(++stable>4)return;}else stable=0;await new Promise(requestAnimationFrame);}throw Error('Target resources did not settle: '+JSON.stringify(ui.stats()));}
 try{await settle();const shot=canvas.toDataURL(),hp=scene.quads.find(q=>q.texture.endsWith('tw_hp.png')),frame=scene.quads.find(q=>q.texture.endsWith('window_all.png')&&q.rect[2]===196&&q.rect[3]===78);
 const actual=document.createElement('canvas');actual.width=1024;actual.height=768;const a=actual.getContext('2d');a.drawImage(canvas,0,0);
 const expected=document.createElement('canvas');expected.width=196;expected.height=78;const e=expected.getContext('2d'),atlas=await createImageBitmap(await(await fetch(frame.texture)).blob());e.drawImage(atlas,frame.uv[0]*atlas.width,frame.uv[1]*atlas.height,frame.uv[2]*atlas.width,frame.uv[3]*atlas.height,0,0,196,78);atlas.close();
 const errors=[[4,30],[190,40],[50,48],[100,74]].map(([x,y])=>{const aa=a.getImageData(frame.rect[0]+x,frame.rect[1]+y,1,1).data,bb=e.getImageData(x,y,1,1).data;return Math.max(...[0,1,2].map(i=>Math.abs(aa[i]-bb[i])));});
 entity.kind='npc';state.entities=[{...entity}];await settle();const npc=semantics.controls.find(c=>c.id==='clear-target').rect;
 entity.kind='player';entity.jobType=4;entity.countryByte9c=1;state.entities=[{...entity}];await settle();const player=scene.quads.filter(q=>q.texture.endsWith('window_all.png')&&q.rect[2]===196&&q.rect[3]===36).map(q=>q.rect),oldHp=scene.quads.some(q=>q.texture.endsWith('tw_hp.png'));
 return {shot,errors,hp:hp.rect,frame:frame.rect,npc,player,oldHp,failed:ui.stats().failed};
 }finally{ui.dispose();renderer.dispose();assets.dispose();canvas.remove();}
 });await mkdir('temp/artifacts/target-status',{recursive:true});await writeFile('temp/artifacts/target-status/monster.png',Buffer.from(result.shot.split(',')[1],'base64'));await writeFile('temp/artifacts/target-status/result.json',JSON.stringify({...result,shot:undefined},null,2));
 assert.deepEqual(result.frame,[414,7,196,78]);assert.equal(result.hp[2],84);assert.ok(Math.max(...result.errors)<=4,JSON.stringify(result.errors));assert.deepEqual(result.npc,[610,16,16,16]);assert.deepEqual(result.player,[[414,7,196,36]]);assert.equal(result.oldHp,false);assert.deepEqual(result.failed,[]);
 }finally{await browser.close();}
});
