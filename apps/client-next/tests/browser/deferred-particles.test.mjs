import {test} from 'node:test';
import assert from 'node:assert/strict';
import {launchProbeBrowser} from '../../../../scripts/lib/probeBrowser.mjs';
import {CLIENT_NEXT_BASE_URL} from '../../../../scripts/lib/probeEndpoints.mjs';
test('production renderer fades deferred pixels on delivered visibility and preserves foreground depth',{timeout:120000},async()=>{
 const {browser,page}=await launchProbeBrowser();try{await page.goto(CLIENT_NEXT_BASE_URL);
 const result=await page.evaluate(async()=>{
  const {createRenderer}=await import('/src/engine/runtime/renderer/renderer.ts');const {defaultVideoOptions,changeVideo}=await import('/src/engine/foundation/rendering/video-options.ts');const canvas=document.createElement('canvas');document.body.append(canvas);const renderer=createRenderer(canvas),I=()=>Float32Array.of(1,0,0,0,0,1,0,0,0,0,1,0,0,0,0,1);
  const model=(color,blend)=>({nodes:[{name:'root',parent:-1,translation:[0,0,0],rotation:[0,0,0,1],scale:[1,1,1]}],images:[],clips:[{name:'effect',duration:10,channels:[]}],primitives:[{name:'mesh',node:0,joints:[0],inverseBind:I(),image:-1,geometry:{positions:Float32Array.of(-20,-20,0,20,-20,0,20,20,0,-20,20,0),indices:Uint32Array.of(0,1,2,0,2,3),joints:new Uint32Array(16),weights:Float32Array.of(1,0,0,0,1,0,0,0,1,0,0,0,1,0,0,0),transform:I(),material:{color,alphaCutoff:0,blend,doubleSided:true,unlit:true}}}]});
  const effect={gid:1,model:'effect',pose:{regionId:257,x:0,y:0,z:0,yaw:0},clip:'effect',time:0,loop:true,scale:1,deferredParticle:{offset:1}},wall={...effect,gid:2,model:'wall',deferredParticle:undefined,pose:{...effect.pose,z:5}};
  const copy=document.createElement('canvas');copy.width=copy.height=64;const ctx=copy.getContext('2d');
  async function sample(time,occluded=false){renderer.setCharacterActors([{...effect,time},...(occluded?[wall]:[])]);await renderer.frame({width:64,height:64},time);// #2573: no frame waits on the readback; the answer lands between frames, so give it a frame turn and a settle before reading pixels - the pacing the runtime itself runs at.
await new Promise(requestAnimationFrame);await new Promise(r=>setTimeout(r,50));const image=await createImageBitmap(canvas);ctx.drawImage(image,0,0);image.close();return [...ctx.getImageData(32,32,1,1).data];}
  try{const deadline=performance.now()+15000;while(renderer.phase()==='starting'){if(performance.now()>deadline)throw Error('GPU deadline');await new Promise(requestAnimationFrame);}
   renderer.setWorld({id:'deferred',originRegion:257,warnings:[],groups:[]});renderer.setWorldCamera({originRegion:257,eye:[0,0,30],target:[0,0,0],fov:1,near:1,far:100});renderer.setCharacterModel('effect',model([1,0,0,1],true),[]);renderer.setCharacterModel('wall',model([0,0,1,1],false),[]);
   const samples=[await sample(0),await sample(.5),await sample(.501),await sample(.601),await sample(.801),await sample(1,true),await sample(1.001,true),await sample(1.202,true),await sample(1.503)];
   renderer.videoOptions(changeVideo(defaultVideoOptions(),7,0));samples.push(await sample(1.603),await sample(1.653,true));
   renderer.videoOptions(defaultVideoOptions());samples.push(await sample(1.703),await sample(2.005));return samples;
  }finally{renderer.dispose();canvas.remove();}
 });assert.deepEqual(result[0],result[1]);assert.deepEqual(result[2],result[0]);assert.ok(result[3][0]>100);assert.ok(result[4][0]>=254);assert.ok(result[5][0]>=254);assert.ok(result[6][0]>=250&&result[6][2]<=5);assert.deepEqual(result[7],[0,0,255,255]);
// #2573: the re-ask frame [8] presents the last delivered (invisible) state - its answer lands between frames. The option-off frame [9] therefore draws the immediate route at the last known alpha (none yet), and the restored deferred route fades back in over [11] instead of popping to full.
assert.ok(result[8][0]<100);assert.ok(result[9][0]<100);assert.deepEqual(result[10],[0,0,255,255]);assert.ok(result[11][0]>25&&result[11][0]<254);assert.ok(result[12][0]>=254);
 }finally{await browser.close();}
});
