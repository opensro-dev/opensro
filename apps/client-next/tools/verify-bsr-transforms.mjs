import fs from 'node:fs';
import {createHash} from 'node:crypto';
import {spawnSync} from 'node:child_process';
import {build} from 'esbuild';
import {pythonExecutable} from '../../../scripts/build/shared/pythonRun.mjs';
const source='src/engine/foundation/animation/bsr-particle-transform.ts';
const r=await build({entryPoints:[source],bundle:true,platform:'node',format:'esm',write:false});
const m=await import('data:text/javascript;base64,'+Buffer.from(r.outputFiles[0].contents).toString('base64'));
const cases=[];for(const x of [0,.2,-.7,Math.PI/2,Math.PI])for(const y of [0,.3,-1.2])for(const z of [0,.4,-Math.PI/2]){const angles=[x,y,z];cases.push({angles,matrix:[...m.bsrParticleRotation(angles)]});}
const transforms=[];
for(const scale of [.5,1,2])for(const root of [true,false])for(const rotated of [true,false]){
 const world=m.bsrParticleRotation([0,.7,0]);world.set([100,200,300],12);
 const bone=root?null:m.bsrParticleRotation([.4,.2,0]);bone?.set([7,8,9],12);
 const offset=[2,3,4],rotation=rotated?m.bsrParticleRotation([.2,.3,.4]):undefined;
 const result=m.bsrParticleTransform(world,bone,offset,scale,rotation);
 transforms.push({scale,world:[...world],bone:bone?[...bone]:null,offset,rotation:rotation?[...rotation]:null,matrix:[...result.matrix]});
}
fs.mkdirSync('temp/artifacts/bsr-parity',{recursive:true});
fs.writeFileSync('temp/artifacts/bsr-parity/rotation-candidates.json',JSON.stringify({sourceSha256:createHash('sha256').update(fs.readFileSync(source)).digest('hex'),cases,transforms}));
const PY=pythonExecutable();const result=spawnSync(PY.command,[...PY.args,'tools/verify-native-bsr-transforms.py'],{encoding:'utf8',timeout:120000});
process.stdout.write(result.stdout??'');process.stderr.write(result.stderr??'');if(result.status!==0)process.exitCode=1;
