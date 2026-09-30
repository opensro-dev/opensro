"""
===========================================================================

verify-native-bsr-transforms.py - Bounded execution of the original AD05F0 and native CRT trigonometry

The candidate is frozen in the input artifact before executing original bytes.

SRO_Client.exe is read from the game root (scripts/sro_paths.py).

===========================================================================
"""
import hashlib,json,struct
from pathlib import Path
import sys
sys.path.insert(0, str(Path(__file__).resolve().parents[3] / 'scripts'))
from sro_paths import GAME_ROOT  # noqa: E402  (SRO_GAME_ROOT or beside the main checkout)
import pefile
from unicorn import Uc,UC_ARCH_X86,UC_MODE_32,UC_HOOK_CODE
from unicorn.x86_const import UC_X86_REG_ESP,UC_X86_REG_ECX,UC_X86_REG_EIP,UC_X86_REG_FPCW,UC_X86_REG_EAX
ROOT=Path(__file__).resolve().parents[1]
candidate=ROOT/'src/engine/foundation/animation/bsr-particle-transform.ts'
frozen=json.loads((ROOT/'temp/artifacts/bsr-parity/rotation-candidates.json').read_text())
assert frozen['sourceSha256']==hashlib.sha256(candidate.read_bytes()).hexdigest()
raw=(GAME_ROOT/'SRO_Client.exe').read_bytes()
assert hashlib.sha256(raw).hexdigest()=='375e868234437e815af8ce9289ddea7ec9144430f4ea24e32988a6d6c9dd108a'
pe=pefile.PE(data=raw);base=pe.OPTIONAL_HEADER.ImageBase
m=Uc(UC_ARCH_X86,UC_MODE_32);m.mem_map(base,(pe.OPTIONAL_HEADER.SizeOfImage+4095)&~4095);m.mem_write(base,pe.get_memory_mapped_image())
m.mem_map(0x10000000,0x10000);sp=0x10008000;out=0x10001000;angles=0x10002000;stop=0x1000f000
results=[]
for case in frozen['cases']:
 m.mem_write(sp,struct.pack('<II',stop,angles));m.mem_write(angles,struct.pack('<fff',*case['angles']));m.mem_write(out,b'\0'*64)
 m.reg_write(UC_X86_REG_FPCW,0x37f);m.reg_write(UC_X86_REG_ESP,sp);m.reg_write(UC_X86_REG_ECX,out)
 m.emu_start(0xad05f0,stop,count=100000)
 assert m.reg_read(UC_X86_REG_EIP)==stop and m.reg_read(UC_X86_REG_ESP)==sp+8
 actual=struct.unpack('<16f',m.mem_read(out,64));error=max(abs(a-b) for a,b in zip(actual,case['matrix']))
 assert error<=1e-6,(case,error,actual)
 results.append({'angles':case['angles'],'maxAbsoluteError':error})
transforms=[]
for case in frozen['transforms']:
 obj=0x10003000;owner=0x10004000;vt=0x10005000;world=0x10006000;effect=0x10006100;evt=0x10006200;bone=0x10006300;bone_data=0x10006400
 def u32(at,n):m.mem_write(at,struct.pack('<I',n))
 def floats(at,values):m.mem_write(at,struct.pack('<'+'f'*len(values),*values))
 m.mem_write(obj,b'\0'*0xd0);u32(obj+0x5c,owner);u32(owner,vt);u32(vt+0x18,0x10007000)
 u32(obj+0x54,effect);u32(effect,evt);u32(evt+0x28,0x10007010);u32(evt+0x1c,0x10007020)
 floats(world,case['world']);floats(obj+0xc0,[case['scale']]);offset=case['offset']
 floats(obj+0x94,[offset[0],struct.unpack('<f',struct.pack('<f',offset[1]*case['scale']))[0],offset[2]])
 if case['bone'] is not None:u32(obj+0x50,bone);u32(bone+0x80,bone_data);floats(bone_data+0x40,case['bone'])
 if case['rotation'] is not None:m.mem_write(obj+8,b'\1');floats(obj+0xc,case['rotation'])
 captured={}
 def hook(machine,address,size,data):
  if address not in [0x10007000,0x10007010,0x10007020]:return
  stack=machine.reg_read(UC_X86_REG_ESP);ret=struct.unpack('<I',machine.mem_read(stack,4))[0]
  if address==0x10007000:machine.reg_write(UC_X86_REG_EAX,world);cleanup=0
  elif address==0x10007010:captured['scale']=struct.unpack('<f',machine.mem_read(stack+4,4))[0];cleanup=4
  else:
   pointer=struct.unpack('<I',machine.mem_read(stack+4,4))[0];captured['matrix']=struct.unpack('<16f',machine.mem_read(pointer,64));cleanup=4
  machine.reg_write(UC_X86_REG_ESP,stack+4+cleanup);machine.reg_write(UC_X86_REG_EIP,ret)
 handle=m.hook_add(UC_HOOK_CODE,hook);u32(sp,stop);m.reg_write(UC_X86_REG_ESP,sp);m.reg_write(UC_X86_REG_ECX,obj)
 m.emu_start(0xaec1f0,stop,count=100000);m.hook_del(handle)
 assert m.reg_read(UC_X86_REG_EIP)==stop and m.reg_read(UC_X86_REG_ESP)==sp+4
 error=max(abs(a-b) for a,b in zip(captured['matrix'],case['matrix']))
 assert captured['scale']==case['scale'] and error<=1e-5,(case,captured,error)
 transforms.append({'root':case['bone'] is None,'rotated':case['rotation'] is not None,'scale':case['scale'],'maxAbsoluteError':error})
target=ROOT/'temp/artifacts/bsr-parity/rotation-native-validation.json'
target.write_text(json.dumps({'sourceSha256':frozen['sourceSha256'],'entries':[0xad05f0,0xaec1f0],'cases':results,'transforms':transforms},indent=2)+'\n')
print(json.dumps({'rotations':len(results),'transforms':len(transforms),'maxRotationError':max(r['maxAbsoluteError'] for r in results),'maxTransformError':max(r['maxAbsoluteError'] for r in transforms)}))
