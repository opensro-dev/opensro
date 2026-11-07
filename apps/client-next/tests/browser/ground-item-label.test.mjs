import {test} from 'node:test';
import assert from 'node:assert/strict';
import {mkdir,writeFile} from 'node:fs/promises';
import {launchProbeBrowser} from '../../../../scripts/lib/probeBrowser.mjs';
import {CLIENT_NEXT_BASE_URL} from '../../../../scripts/lib/probeEndpoints.mjs';
import {holdProbeRuntime} from './helpers/hold-runtime.mjs';
test('production drop names retain native backing and centered glyphs on the GPU',{timeout:45000},async()=>{
 const {browser,page}=await launchProbeBrowser();try{
  await holdProbeRuntime(page);await page.goto(CLIENT_NEXT_BASE_URL);
  const result=await page.evaluate(async()=>{
   const {createUi}=await import('/src/engine/runtime/ui/ui.ts'),{createAssets}=await import('/src/engine/runtime/assets/assets.ts'),{createRenderer}=await import('/src/engine/runtime/renderer/renderer.ts'),{projectCharacterLabels}=await import('/src/engine/foundation/ui/character-labels.ts');
   const canvas=document.createElement('canvas'),renderer=createRenderer(canvas),assets=createAssets();let scene;
   const ui=createUi({assets,commands:()=>{},publish:s=>{scene=s;},texture:(id,image)=>renderer.setUiTexture(id,image),base:location.origin,apiBase:'http://fixture.invalid'});
   const entity={gid:1,kind:'local-player',regionId:257,x:0,y:0,z:0,heading:0,name:'Player'};
   const state={session:{phase:'world',revision:1,character:'Player'},gameplay:{localGid:1,pose:{...entity,angle:0},vitals:[],inventory:[],target:0},entities:[entity,{...entity,gid:123,kind:'ground-item',x:20,name:'Gold',groundItem:{typeFlags:0x2ec,goldAmount:43,tint:0}}],width:512,height:256,worldReady:true,dropNamesHeld:true};
   try{
    const deadline=performance.now()+20000;let labels=[];
    while(performance.now()<deadline){ui.step({...state},performance.now());labels=scene?.quads.filter(q=>q.characterAnchor===123)??[];if(labels.length>1&&ui.stats().pending===0&&renderer.phase()==='running')break;await new Promise(requestAnimationFrame);}
    if(labels.length<2)throw Error('Ground label admission failed '+JSON.stringify(ui.stats()));
    const projected=projectCharacterLabels({...scene,quads:labels},new Map([[123,[256.75,128.75,.5]]]));
    const backing=projected.quads[0],base={depth:.9,rect:[0,0,512,256],clip:[0,0,512,256],uv:[0,0,1,1],texture:'',color:[.5,.5,.5,1]};
    renderer.setUi({...scene,quads:[base,...projected.quads]});renderer.frame({width:512,height:256});if(renderer.error())throw Error(renderer.error());
    const output=document.createElement('canvas');output.width=512;output.height=256;const ctx=output.getContext('2d');const bitmap=await createImageBitmap(canvas);ctx.drawImage(bitmap,0,0);bitmap.close();const pixels=ctx.getImageData(0,0,512,256).data;
    const pixel=(x,y)=>Array.from(pixels.slice((y*512+x)*4,(y*512+x)*4+4));
    const [x,y,w,h]=backing.rect;let bright=0;for(let py=y+1;py<y+h-1;py++)for(let px=x+1;px<x+w-1;px++)if(pixel(px,py)[0]>150)bright++;
    state.dropNamesHeld=false;ui.step({...state},performance.now());const released=scene.quads.filter(q=>q.characterAnchor===123).length;
    const png=output.toDataURL();
    state.gameplay={...state.gameplay,chat:{lines:[{sequence:1,channel:1,gid:1,name:'Player',text:'W'.repeat(55)}]}};
    ui.step({...state},performance.now());
    const speech=projectCharacterLabels({...scene,quads:scene.quads.filter(q=>q.characterAnchor===1)},new Map([[1,[256.75,210.75,.5]]]));
    const boards=speech.quads.filter(q=>!q.texture),speechBoard=boards.reduce((a,b)=>a.rect[3]>b.rect[3]?a:b);
    const atlas=await (await fetch('/assets/fonts/native-ui-font-atlas.json')).json(),font=atlas.fonts['0'],lineHeight=font.recordHeight+5,lines=Math.ceil(55/Math.floor(200/font.glyphs['87'].advanceX));
    renderer.setUi({...scene,quads:[base,...speech.quads]});renderer.frame({width:512,height:256});const speechBitmap=await createImageBitmap(canvas);ctx.drawImage(speechBitmap,0,0);speechBitmap.close();
    const raster=ctx.getImageData(0,0,512,256).data,litRows=[];for(let py=speechBoard.rect[1]+1;py<speechBoard.rect[1]+speechBoard.rect[3]-1;py++){let lit=false;for(let px=speechBoard.rect[0]+1;px<speechBoard.rect[0]+speechBoard.rect[2]-1;px++)if(raster[(py*512+px)*4]>150)lit=true;if(lit)litRows.push(py);}
    const starts=litRows.filter((y,i)=>!i||y!==litRows[i-1]+1);
    return {labelOcclusion:projected.quads.map(q=>q.occlusion),backing:backing.rect,color:backing.color,inside:pixel(x,y),outside:pixel(x-1,y),bright,released,png,speechPng:output.toDataURL(),speechRect:speechBoard.rect,lineHeight,lines,starts};
   }finally{ui.dispose();assets.dispose();renderer.dispose();}
  });
  await mkdir('temp/artifacts/drop-label-audit',{recursive:true});await writeFile('temp/artifacts/drop-label-audit/observed.json',JSON.stringify({...result,png:undefined,speechPng:undefined},null,2));
  assert.equal(result.backing[3],result.lineHeight+2);assert.equal(result.speechRect[3],result.lines*result.lineHeight+2);assert.equal(result.starts.length,result.lines);for(let i=1;i<result.starts.length;i++)assert.equal(result.starts[i]-result.starts[i-1],result.lineHeight);
  assert.ok(result.labelOcclusion.length>1&&result.labelOcclusion.every(x=>x==='none'));assert.deepEqual(result.color,[0,0,0,64/255]);assert.ok(Math.abs(result.inside[0]-result.outside[0]*(191/255))<=1);assert.ok(result.bright>20);assert.equal(result.released,0);
  await mkdir('temp/artifacts/drop-label-audit',{recursive:true});await writeFile('temp/artifacts/drop-label-audit/label.png',Buffer.from(result.png.split(',')[1],'base64'));delete result.png;await writeFile('temp/artifacts/drop-label-audit/speech.png',Buffer.from(result.speechPng.split(',')[1],'base64'));delete result.speechPng;await writeFile('temp/artifacts/drop-label-audit/gpu.json',JSON.stringify(result,null,2));
 }finally{await browser.close();}
});
