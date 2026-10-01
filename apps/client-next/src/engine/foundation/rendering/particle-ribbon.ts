export interface RibbonPoint {position:readonly number[];color:readonly number[];width:number;}
// AF9020: remove coincident neighbours, duplicate endpoint controls, and sample
// the uniform cubic B-spline at thirds. Colour/width interpolate linearly.
export function ribbonSpline(input:readonly RibbonPoint[]):RibbonPoint[]{
 const points:RibbonPoint[]=[];
 for(const point of input){const previous=points.at(-1);if(!previous||point.position.some((v,i)=>Math.abs(v-previous.position[i]!)>1e-6))points.push(point);}
 if(points.length<2)return [];
 const controls=[points[0]!,...points,points.at(-1)!],result:RibbonPoint[]=[];
 for(let i=0;i<controls.length-3;i++)for(let step=0;step<3;step++){
  const t=step/3,t2=t*t,t3=t2*t,w=[(1-t)**3/6,(3*t3-6*t2+4)/6,(-3*t3+3*t2+3*t+1)/6,t3/6],a=controls[i+1]!,b=controls[i+2]!;
  result.push({position:[0,1,2].map(axis=>w.reduce((sum,weight,j)=>sum+controls[i+j]!.position[axis]!*weight,0)),color:a.color.map((v,j)=>Math.trunc((v*(1-t)+b.color[j]!*t)*255)/255),width:a.width*(1-t)+b.width*t});
 }result.push(points.at(-1)!);return result;
}
// AF8E80 (LinkDPipe) / AF73A0 (LinkObj): the raw element chain, dropping a
// point only when no axis moved by 1e-6 or more; no spline.
export function ribbonPolyline(input:readonly RibbonPoint[]):RibbonPoint[]{
 const points:RibbonPoint[]=[];
 for(const point of input){const previous=points.at(-1);if(!previous||point.position.some((v,i)=>Math.abs(v-previous.position[i]!)>=1e-6))points.push(point);}
 return points.length<2?[]:points;
}
// AF80E0: projected neighbour tangent, camera-space perpendicular, world-space
// half-width. Rizin AF8103..AF811E confirms UV step = 1/(pointCount-1).
// The tangent is NDC, not pixels: AF80E0 calls D3DXVec3Project with a NULL
// viewport, which stops at normalized device coordinates.
export function ribbonStrip(points:readonly RibbonPoint[],view:Float32Array){
 const positions=new Float32Array(points.length*6),colors=new Float32Array(points.length*8),uvs=new Float32Array(points.length*4),indices=new Uint32Array(Math.max(0,points.length-1)*6);
 const right=[view[0]!,view[4]!,view[8]!],up=[view[1]!,view[5]!,view[9]!];
 const rl=Math.hypot(...right),ul=Math.hypot(...up);if(rl<1e-12||ul<1e-12)throw Error('Invalid ribbon camera');
 const projected=points.map(p=>{const [x,y,z]=p.position as readonly [number,number,number],w=view[3]!*x+view[7]!*y+view[11]!*z+view[15]!;return [(view[0]!*x+view[4]!*y+view[8]!*z+view[12]!)/(w||1e-12),(view[1]!*x+view[5]!*y+view[9]!*z+view[13]!)/(w||1e-12)];});
 for(let i=0;i<points.length;i++){
  const a=projected[Math.max(0,i-1)]!,b=projected[Math.min(points.length-1,i+1)]!,dx=a[0]!-b[0]!,dy=a[1]!-b[1]!,side=right.map((v,j)=>v/rl*dy-up[j]!/ul*dx),length=Math.hypot(...side),p=points[i]!;
  for(let j=0;j<3;j++){const offset=length>1e-12?side[j]!/length*p.width:0;positions[i*6+j]=p.position[j]!+offset;positions[i*6+3+j]=p.position[j]!-offset;}
  colors.set(p.color,i*8);colors.set(p.color,i*8+4);uvs.set([i/Math.max(1,points.length-1),0,i/Math.max(1,points.length-1),1],i*4);
  if(i)indices.set([i*2-2,i*2-1,i*2,i*2,i*2-1,i*2+1],(i-1)*6);
 }return {positions,colors,uvs,indices};
}
