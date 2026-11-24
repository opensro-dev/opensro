import {build} from 'esbuild';
import {spawnSync} from 'node:child_process';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import {pythonExecutable} from '../../../scripts/build/shared/pythonRun.mjs';
const buildResult=await build({entryPoints:['src/engine/foundation/animation/animation-dispatch.ts'],bundle:true,platform:'node',format:'esm',write:false});
const {createAnimationDispatch}=await import('data:text/javascript;base64,'+Buffer.from(buildResult.outputFiles[0].contents).toString('base64'));
const cases=[];
for(const durations of [[1000,2000],[333,777],[1,3],[999,1001]])for(const weights of [[1,1],[.1,.3],[0,1],[.33333334,.6666667],[0,0]])cases.push({layers:durations.map((duration,i)=>({duration,weight:weights[i]})),steps:[0,4,16,199,500,3000,1,1000]});
for(const durations of [[1000,2000],[333,777]])cases.push({layers:durations.map(duration=>({duration,weight:1})),steps:[16,199,500,4,16,100,50,3000],weights:[[1,0],[.9,.1],[.1,.9],[0,1],[.3,.7],[.8,.2],[1,0],[.5,.5]]});
cases.push({layers:[{duration:600,weight:1},{duration:4000,weight:0}],steps:[350,...Array(49).fill(4)],weights:[[1,0],...Array.from({length:49},(_,i)=>[1-(i+1)/50,(i+1)/50])]});
// Exact boundaries, stationary equality and the subsequent overflow wrap.
for(const duration of [1,333,1000])cases.push({layers:[{duration,weight:1}],steps:[duration-1,1,0,1,1,duration,0]});
const PY=pythonExecutable();const native=spawnSync(PY.command,[...PY.args,'tools/native-animation-dispatch.py'],{input:JSON.stringify(cases),encoding:'utf8',timeout:60000});
if(native.status!==0)throw Error(native.stderr);
const actual=cases.map(c=>{const owner=createAnimationDispatch(),layers=c.layers.map((l,i)=>({clip:String(i),time:0,weight:l.weight,loop:true,lane:'timed',activation:Object.freeze({started:0})}));return c.steps.map((dt,index)=>{if(c.weights)for(let i=0;i<layers.length;i++)layers[i].weight=c.weights[index][i];return owner.step(layers,dt,clip=>c.layers[Number(clip)].duration).flatMap(row=>row.ranges.map(([from,to])=>[Number(row.layer.clip),from,to]));});});
const expected=JSON.parse(native.stdout);
// The port wraps its cursor on `>=` where AE05F0 wraps on `>`, so the original
// dispatches one extra whole cycle whenever a cursor lands exactly on the clip
// length. That is the ONLY sanctioned difference: a frame is accepted when the
// original's ranges equal the port's with exactly one full `[0,length]` cycle
// removed per layer, and it is counted. Every other difference fails.
const repeatedCycle=(want,got,durations)=>{
 const group=rows=>rows.reduce((m,r)=>m.set(r[0],[...(m.get(r[0])??[]),r]),new Map());
 const w=group(want),g=group(got);
 for(const key of new Set([...w.keys(),...g.keys()])){
  const rows=[...(w.get(key)??[])],mine=g.get(key)??[];
  const at=rows.findIndex(([,from,to])=>from===0&&to===durations[key]);
  if(at<0)return false;
  rows.splice(at,1);
  if(JSON.stringify(rows)!==JSON.stringify(mine))return false;
 }
 return true;
};
let deviations=0;
cases.forEach((c,ci)=>c.steps.forEach((dt,fi)=>{
 const want=expected[ci][fi],got=actual[ci][fi];
 if(JSON.stringify(want)===JSON.stringify(got))return;
 assert.ok(repeatedCycle(want,got,c.layers.map(l=>l.duration)),
  `case ${ci} frame ${fi} differs beyond the equality-boundary repeated cycle`);
 deviations++;
}));
const report={functionVas:['ADD670','AE05D0','AE0450'],cases:cases.length,frames:cases.reduce((n,c)=>n+c.steps.length,0),differences:0,documentedDeviations:deviations,qualification:'Original-instruction comparison of timed-list group arithmetic, installation cursor update and dispatch ranges in constructed state 3 at rate 1. Zero unexplained differences. documentedDeviations counts frames where the original repeats one whole cycle after a cursor lands exactly on the clip length (AE05F0 wraps on >, AE05BC retains modulo); the port wraps on >= so a high-refresh delta does not repeat it every cycle. Each such frame is asserted to differ by exactly one full cycle per layer. Key/sound/motion receivers are boundaries; event-lane lifetimes remain presenter-owned.'};
fs.writeFileSync('temp/artifacts/bsr-parity/animation-dispatch-native.json',JSON.stringify(report,null,2)+'\n');console.log(JSON.stringify(report));
