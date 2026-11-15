import {identity} from '@/engine/foundation/rendering/world-math';
import {multiply} from '@/engine/foundation/math/pose-math';

// AF97B0: AngleVector1 stores min/max plus authored degrees; right is scratch.
export function particleCone(parameter:{kind:string;left?:unknown;right?:unknown}|undefined):number[]{
 const source=parameter?.kind==='AngleVector1'?parameter.left:parameter?.right;
 if(!Array.isArray(source)||source.length!==3||source.some(n=>typeof n!=='number'||!Number.isFinite(n)))throw Error('Invalid particle cone parameter');
 return parameter?.kind==='AngleVector1'?[source[0],source[1],Math.fround(source[2]*3.1415927410125732/180)]:[...source];
}

// AFE0D0/AFE120 bind conversion callbacks AFB820/AFD030. The serialized
// right-hand matrix is scratch, not the evaluated AxisVector4/RotVector value.
export function particleRotation(parameter:{kind:string;left?:unknown;right?:unknown;value?:unknown}|undefined):number[]{
 const vector=(value:unknown,length:number)=>{if(!Array.isArray(value)||value.length!==length||value.some(n=>typeof n!=='number'||!Number.isFinite(n)))throw Error('Invalid particle rotation parameter');return value as number[];};
 const radians=(degrees:number)=>Math.fround(degrees*3.1415927410125732/180);
 if(parameter?.kind==='AxisVector4'){
  const [ax,ay,az,degrees]=vector(parameter.left,4) as [number,number,number,number];
  const length=Math.fround(Math.sqrt(Math.fround(ax*ax+ay*ay+az*az)));
  // AFAB10 divides the axis by its length unchecked: a zero axis gives NaN
 // in every 3x3 entry (masked x87 0/0), and the node and its children draw
 // nothing. A zero basis is the finite form of that: the geometry collapses
 // to one point. dun/bagh_masin_bridge01_l.efp authors one.
 if(!length){const out=identity();out[0]=out[5]=out[10]=0;return Array.from(out);}
  const x=Math.fround(ax/length),y=Math.fround(ay/length),z=Math.fround(az/length),angle=-radians(degrees);
  const c=Math.fround(Math.cos(angle)),s=Math.fround(Math.sin(angle)),t=Math.fround(1-c),out=identity();
  out[0]=x*x*t+c;out[5]=y*y*t+c;out[10]=z*z*t+c;
  out[4]=x*y*t+z*s;out[8]=x*z*t-y*s;out[1]=x*y*t-z*s;
  out[9]=y*z*t+x*s;out[2]=x*z*t+y*s;out[6]=y*z*t-x*s;
  return Array.from(out);
 }
 if(parameter?.kind==='RotVector'){
  const [pitch,yaw,roll]=vector(parameter.left,3) as [number,number,number],matrices=[identity(),identity(),identity()];
  for(const [i,degrees]of [pitch,yaw,roll].entries()){
   const c=Math.fround(Math.cos(radians(degrees))),s=Math.fround(Math.sin(radians(degrees))),m=matrices[i]!;
   if(i===0){m[5]=c;m[6]=s;m[9]=-s;m[10]=c;}
   else if(i===1){m[0]=c;m[2]=s;m[8]=-s;m[10]=c;}
   else{m[0]=c;m[1]=s;m[4]=-s;m[5]=c;}
  }
  // Native row-vector X * Y * Z becomes column-vector Z * Y * X.
  const first=identity(),out=identity();multiply(matrices[1]!,matrices[0]!,first);multiply(matrices[2]!,first,out);return Array.from(out);
 }
 return [...vector(parameter?.value??parameter?.right,16)];
}
