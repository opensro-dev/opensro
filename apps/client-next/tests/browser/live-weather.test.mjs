import {test} from 'node:test';
import assert from 'node:assert/strict';
import {mkdir,writeFile} from 'node:fs/promises';
import {launchProbeBrowser} from '../../../../scripts/lib/probeBrowser.mjs';
import {CLIENT_NEXT_BASE_URL} from '../../../../scripts/lib/probeEndpoints.mjs';
test('rain and snow produce GPU pixels through retained weather geometry and reset cleanly',{timeout:90000},async()=>{
 const {browser,page}=await launchProbeBrowser();try{
 await page.goto(CLIENT_NEXT_BASE_URL);
 const result=await page.evaluate(async()=>{
  const {createDevice}=await import('/src/engine/runtime/renderer/device/device.ts');const {createFrame}=await import('/src/engine/runtime/renderer/frame/frame.ts');const {createWeather}=await import('/src/engine/runtime/renderer/weather/weather.ts');const {createPresentationRandom}=await import('/src/engine/runtime/random/random.ts');
  const device=createDevice(),canvas=document.createElement('canvas');canvas.width=512;canvas.height=384;document.body.append(canvas);
  try{
   for(let n=0;n<300&&device.phase()==='starting';n++)await new Promise(requestAnimationFrame);if(device.phase()!=='running')throw Error(device.error()??'Device startup timeout');
   const ctx=canvas.getContext('webgpu');device.surfaceCommands().configure(ctx,device.format());const depth=device.surfaceCommands().createDepth(512,384),frame=createFrame(device.commands()),images=new Map();
   for(const name of ['rain1','rain2','snow1','snow2']){const path='/assets/images/Map_extracted/weather/'+name+'.png';const response=await fetch(path);if(!response.ok)throw Error('Missing weather texture '+path);const bitmap=await createImageBitmap(await response.blob(),{premultiplyAlpha:'none',colorSpaceConversion:'none'});images.set(path,device.images().upload(bitmap));bitmap.close();}
   const camera={eye:[0,0,0],target:[0,0,1],near:1,far:2000,fov:1},matrix=Float32Array.from([.005,0,0,0,0,.005,0,0,0,0,.001,0,0,0,.5,1]);device.worldView(matrix,new Float32Array(80));
   async function capture(draws){frame.draw(ctx.getCurrentTexture().createView(),undefined,undefined,depth.view,draws);const bitmap=await createImageBitmap(canvas),copy=document.createElement('canvas');copy.width=512;copy.height=384;const c=copy.getContext('2d');c.drawImage(bitmap,0,0);bitmap.close();return c.getImageData(0,0,512,384).data;}
   const background=await capture([]),rows=[];
   for(const mode of [2,3]){const weather=createWeather(createPresentationRandom(77));weather.set({mode,amount:80});let draws=[];for(let i=0;i<=120;i++){weather.update(camera,i/60,0,true);draws=weather.prepare(device.geometry(),images,camera,matrix);};const pixels=await capture(draws);let changed=0;for(let i=0;i<pixels.length;i+=4)if(pixels[i]!==background[i]||pixels[i+1]!==background[i+1]||pixels[i+2]!==background[i+2])changed++;rows.push({mode,changed,draws:draws.length,...weather.stats()});weather.set(null);weather.update(camera,3,0,true);const empty=weather.prepare(device.geometry(),images,camera,matrix);if(empty.length)throw Error('Weather survived reset');weather.dispose(device.geometry());}
   return {rows,error:device.error()};
  }finally{device.dispose();canvas.remove();}
 });
 await mkdir('temp/artifacts/live-weather',{recursive:true});await writeFile('temp/artifacts/live-weather/result.json',JSON.stringify(result,null,2));console.log(JSON.stringify(result));assert.equal(result.error,null);assert.ok(result.rows.every(r=>r.changed>0&&r.draws>0&&r.draws<=2&&r.particles>0));
 }finally{await browser.close();}
});

test('thunder blends the scene through the production device/frame owners',{timeout:90000},async()=>{
 const {browser,page}=await launchProbeBrowser();try{
 await page.goto(CLIENT_NEXT_BASE_URL);
 const result=await page.evaluate(async()=>{
  const {createDevice}=await import('/src/engine/runtime/renderer/device/device.ts');
  const {createFrame}=await import('/src/engine/runtime/renderer/frame/frame.ts');
  const device=createDevice(),canvas=document.createElement('canvas');canvas.width=32;canvas.height=24;document.body.append(canvas);
  try{
   for(let n=0;n<300&&device.phase()==='starting';n++)await new Promise(requestAnimationFrame);
   if(device.phase()!=='running')throw Error(device.error()??'Device startup timeout');
   const context=canvas.getContext('webgpu');device.surfaceCommands().configure(context,device.format());
   const frame=createFrame(device.commands()),depth=device.surfaceCommands().createDepth(32,24),samples=[];
   for(const color of [[1,1,1,0],[156/255,156/255,156/255,128/255],[0,0,0,1]]){
    frame.draw(context.getCurrentTexture().createView(),undefined,undefined,depth.view,[],[],[],undefined,device.thunder(color));
    const bitmap=await createImageBitmap(canvas),copy=document.createElement('canvas');copy.width=32;copy.height=24;const ctx=copy.getContext('2d');ctx.drawImage(bitmap,0,0);bitmap.close();
    samples.push([...ctx.getImageData(16,12,1,1).data]);
   }
   return {samples,error:device.error()};
  }finally{device.dispose();canvas.remove();}
 });
 assert.equal(result.error,null);assert.deepEqual(result.samples[2],[0,0,0,255]);
 const expected=result.samples[0].slice(0,3).map(v=>Math.min(255,Math.round(v*156/255+156*128/255)));
 assert.ok(expected.every((v,i)=>Math.abs(v-result.samples[1][i])<=1));
 await writeFile('temp/artifacts/live-weather/thunder.json',JSON.stringify({...result,scope:'GPU blend equation sanity check; not strict D3D9 or whole-scene acceptance'},null,2));
 }finally{await browser.close();}
});
