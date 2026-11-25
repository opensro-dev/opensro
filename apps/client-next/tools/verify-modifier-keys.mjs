import {build} from 'esbuild';
import {spawnSync} from 'node:child_process';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import {pythonExecutable} from '../../../scripts/build/shared/pythonRun.mjs';
const code=await build({entryPoints:['src/engine/foundation/animation/animation-emission.ts'],bundle:true,platform:'node',format:'esm',write:false});
const {createAnimationEmission,modelAnimationParticles}=await import('data:text/javascript;base64,'+Buffer.from(code.outputFiles[0].contents).toString('base64'));
const cases=[];for(const keys of [[],[0],[0,100,100,999,1000],[381,2240,2732,3206]])for(const ranges of [[[0,0]],[[0,1]],[[0,100]],[[100,101]],[[0,1000]],[[999,1000],[0,100]],[[0,3300]]])cases.push({keys,ranges});
const PY=pythonExecutable();const native=spawnSync(PY.command,[...PY.args,'tools/native-modifier-keys.py'],{input:JSON.stringify(cases),encoding:'utf8',timeout:60000});if(native.status!==0)throw Error(native.stderr);
const actual=cases.map(c=>{let serial=0;const owner=createAnimationEmission(()=>++serial),selector={set:'default',state:0};
 const sets=modelAnimationParticles([{kind:1,stateId:0,animationSetName:'default',baseWords:[1056964608,1,48,4294967295,0,0],entries:c.keys.map((key,i)=>({field00:1,effectPath:'system/test'+i+'.efp',boneName:'',vector3c:[0,0,0],field4c:key,flags50:[0,0,0],flag53:0}))}]);
 const actor={gid:99,model:'body',pose:{regionId:257,x:0,y:0,z:0,yaw:0},scale:1,clip:'stand',time:0,loop:true,modelAnimation:{selected:selector,revision:0,restarted:[],dispatch:[{selector,ranges:c.ranges}]}};
 const rows=owner.step([{actor,sets}],0,()=>true,100,true,()=>null,()=>0,()=>10);
 // Allocation records native time-first key dispatch; rendering retains authored order.
 return rows.sort((a,b)=>a.gid-b.gid).map(r=>Number(/test(\d+)/.exec(r.model)[1]));
});
assert.deepEqual(actual,JSON.parse(native.stdout));
const report={functions:['AE0380','ADF890'],cases:cases.length,differences:0,qualification:'Original key traversal; virtual activation receiver stubbed. Production wrapper admission compared by authored entry.'};
fs.writeFileSync('temp/artifacts/bsr-parity/modifier-keys-native.json',JSON.stringify(report,null,2)+'\n');console.log(JSON.stringify(report));
