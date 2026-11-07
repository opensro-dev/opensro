import {test} from 'node:test';
import assert from 'node:assert/strict';
import {mkdir,writeFile} from 'node:fs/promises';
import {launchProbeBrowser} from '../../../../scripts/lib/probeBrowser.mjs';
import {CLIENT_NEXT_BASE_URL} from '../../../../scripts/lib/probeEndpoints.mjs';
import {holdProbeRuntime} from './helpers/hold-runtime.mjs';

test('loading UI retains entry art through travel metadata and publishes full before reveal',{timeout:60000},async()=>{
 const {browser,page}=await launchProbeBrowser();try{
  await holdProbeRuntime(page);await page.goto(CLIENT_NEXT_BASE_URL);
  const result=await page.evaluate(async()=>{
   const {createAssets}=await import('/src/engine/runtime/assets/assets.ts'),{createUi}=await import('/src/engine/runtime/ui/ui.ts');const assets=createAssets();let scene,draws=0,variants=0;
   const ui=createUi({assets,commands:()=>{},publish:s=>{scene=s;draws++;},texture:()=>{},base:location.origin,apiBase:'http://fixture.invalid',clickSound:()=>{},sound:()=>{},loadingVariant:()=>++variants%2+1});
   let state={frontend:{phase:'loading-world',generation:1,elapsed:0,alpha:0,logoAlpha:0,error:null},session:{phase:'entering-world',revision:1},gameplay:null,entities:[],width:1024,height:768,worldReady:false,loadingProgress:.66};const frames=[];let semantics;
   function frame(now){semantics=ui.step(state,now)??semantics;frames.push({now,progress:semantics?.loadingProgress,background:scene.quads[1]?.texture,gauge:scene.quads.find(q=>q.texture.endsWith('gauge_loading.png'))?.uv[2],variants,draws});}
   try{frame(0);state.loadingProgress=.4;frame(10);state.travel={mode:2,region:0x694f,revision:1};state.loadingProgress=.1;frame(20);state.loadingProgress=.7;frame(30);state.session={phase:'world',revision:2};state.worldReady=true;frame(40);state.frontend={...state.frontend,phase:'world'};state.travel=null;frame(50);frame(141);state.travel={mode:2,region:0x694f,revision:2};state.worldReady=false;frame(200);state.loadingProgress=.5;frame(210);state.travel={mode:1,region:0x694f,revision:3};frame(220);return frames;}finally{ui.dispose();assets.dispose();}
  });
  await mkdir('temp/artifacts/loading-presentation',{recursive:true});await writeFile('temp/artifacts/loading-presentation/transition.json',JSON.stringify(result,null,2));
  assert.deepEqual(result.slice(0,6).map(r=>r.progress),[0,.4,.4,.7,1,1]);assert.equal(new Set(result.slice(0,6).map(r=>r.background)).size,1);assert.equal(result[5].variants,1);assert.equal(result[5].gauge,1);assert.equal(result[6].gauge,undefined);assert.equal(result[7].gauge,0);assert.equal(result[8].gauge,.5);assert.equal(result[9].gauge,0);assert.ok(result[9].background.endsWith("loading_rebirth.png"));
 }finally{await browser.close();}
});

test('real startup shows stage status and full progress before hiding its overlay',{timeout:90000},async()=>{
 const {browser,page}=await launchProbeBrowser();try{
  await holdProbeRuntime(page);await page.addInitScript(()=>{window.loadingSamples=[];const observe=()=>{const e=document.querySelector('.sro-boot-loading');if(e)window.loadingSamples.push({active:e.dataset.active,progress:e.style.getPropertyValue('--loading-progress'),detail:e.querySelector('.sro-boot-loading__detail')?.textContent});};new MutationObserver(observe).observe(document,{subtree:true,attributes:true,childList:true,characterData:true});});
  await page.goto(CLIENT_NEXT_BASE_URL);await page.waitForFunction(()=>document.querySelector('.sro-boot-loading')?.dataset.active==='false',null,{timeout:75000});
  const samples=await page.evaluate(()=>window.loadingSamples);await mkdir('temp/artifacts/loading-presentation',{recursive:true});await writeFile('temp/artifacts/loading-presentation/startup.json',JSON.stringify(samples,null,2));
  assert.ok(samples.some(s=>/Loading scene|Loading textures|Preparing scene graphics/.test(s.detail)));assert.ok(samples.some(s=>s.active==='true'&&Number(s.progress)===1));assert.equal(samples.at(-1).progress,'1');assert.equal(samples.at(-1).detail,'Ready');const active=samples.filter(s=>s.active==='true'&&s.progress!=='').map(s=>Number(s.progress));for(let i=1;i<active.length;i++)assert.ok(active[i]>=active[i-1]);
 }finally{await browser.close();}
});

test('world resource admission uses native loading artwork after frontend handoff and without frontend metadata',{timeout:60000},async()=>{
 const {browser,page}=await launchProbeBrowser();try{
  await holdProbeRuntime(page);await page.goto(CLIENT_NEXT_BASE_URL);
  const rows=await page.evaluate(async()=>{
   const {createAssets}=await import('/src/engine/runtime/assets/assets.ts'),{createUi}=await import('/src/engine/runtime/ui/ui.ts');const rows=[];
   for(const frontend of [undefined,{phase:'world',generation:1,elapsed:0,alpha:0,logoAlpha:0,error:null}]){
    const assets=createAssets();let scene;const ui=createUi({assets,commands:()=>{},publish:s=>scene=s,texture:()=>{},base:location.origin,apiBase:'http://fixture.invalid',clickSound:()=>{},sound:()=>{},loadingVariant:()=>1});
    const state={frontend,session:{phase:'world',revision:1,character:'Fixture'},gameplay:null,entities:[],width:1024,height:768,worldReady:false,loadingProgress:.4};
    try{
     ui.step(state,0);const pending=ui.step(state,10);
     rows.push({art:scene.quads.some(q=>q.texture.includes('/interface/loading/')),gauge:scene.quads.find(q=>q.texture.endsWith('gauge_loading.png'))?.uv[2],logout:pending?.controls?.some(c=>c.id==='logout')??false});
     state.worldReady=true;ui.step(state,20);rows.push({full:scene.quads.find(q=>q.texture.endsWith('gauge_loading.png'))?.uv[2]});
     ui.step(state,121);rows.push({retired:!scene.quads.some(q=>q.texture.endsWith('gauge_loading.png'))});
    }finally{ui.dispose();assets.dispose();}
   }return rows;
  });
  for(let i=0;i<rows.length;i+=3){assert.deepEqual(rows[i],{art:true,gauge:.4,logout:false});assert.deepEqual(rows[i+1],{full:1});assert.deepEqual(rows[i+2],{retired:true});}
  await mkdir('temp/artifacts/loading-presentation',{recursive:true});await writeFile('temp/artifacts/loading-presentation/world-handoff.json',JSON.stringify(rows,null,2));
 }finally{await browser.close();}
});
