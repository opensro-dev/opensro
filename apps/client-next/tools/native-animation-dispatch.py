"""
===========================================================================

native-animation-dispatch.py - Original ADD670/AE0450; clip updates and key/sound/motion receivers are boundaries

SRO_Client.exe is read from the game root (scripts/sro_paths.py).

===========================================================================
"""
import hashlib,json,struct,sys
from pathlib import Path
sys.path.insert(0, str(Path(__file__).resolve().parents[3] / 'scripts'))
from sro_paths import GAME_ROOT  # noqa: E402  (SRO_GAME_ROOT or beside the main checkout)
import pefile
from unicorn import Uc,UC_ARCH_X86,UC_MODE_32,UC_HOOK_CODE
from unicorn.x86_const import UC_X86_REG_ESP,UC_X86_REG_ECX,UC_X86_REG_EAX,UC_X86_REG_EIP,UC_X86_REG_FPCW
root=Path(__file__).resolve().parents[1]
raw=(GAME_ROOT/'SRO_Client.exe').read_bytes()
assert hashlib.sha256(raw).hexdigest()=='375e868234437e815af8ce9289ddea7ec9144430f4ea24e32988a6d6c9dd108a'
pe=pefile.PE(data=raw);base=pe.OPTIONAL_HEADER.ImageBase
out=[]
for case in json.load(sys.stdin):
 m=Uc(UC_ARCH_X86,UC_MODE_32);m.mem_map(base,(pe.OPTIONAL_HEADER.SizeOfImage+4095)&~4095);m.mem_write(base,pe.get_memory_mapped_image())
 arena=0x10000000;m.mem_map(arena,0x40000);obj=arena+0x1000;head=arena+0x2000;eventhead=head+0x20;vt=head+0x40;stub=head+0x80;stop=arena+0x3f000;sp=arena+0x30000
 def put(at,n):m.mem_write(at,struct.pack('<I',n))
 def get(at):return struct.unpack('<I',m.mem_read(at,4))[0]
 def ret(size=0):
  s=m.reg_read(UC_X86_REG_ESP);m.reg_write(UC_X86_REG_ESP,s+4+size);m.reg_write(UC_X86_REG_EIP,get(s))
 put(obj+0x28,head);put(obj+0x34,eventhead);put(eventhead,eventhead);put(eventhead+4,eventhead);put(vt+8,0xae05d0)
 m.mem_write(stub,b'\x31\xc0\xc2\x0c\x00');m.mem_write(stub+16,b'\xd9\xee\xc2\x08\x00')
 addresses={};previous=head
 for i,row in enumerate(case['layers']):
  node=arena+0x3000+i*0x400;ani=node+0x40;ban=node+0x180;addresses[ani]=i
  put(previous,node);put(node+4,previous);put(node+8,ani);previous=node
  put(ani,vt);put(ani+8,ban);put(ban+0x58,row['duration']);put(ban+0xa0,1)
  put(ani+0x10,3);put(ani+0x88,1000000)
  m.mem_write(ani+0x34,struct.pack('<ff',1,row['weight']))
  m.mem_write(ani+0x8c,struct.pack('<f',row['weight']))
 put(previous,head);put(head+4,previous)
 ranges=[]
 def hook(machine,address,size,user):
  if address==0xae0380:
   s=m.reg_read(UC_X86_REG_ESP);ranges.append([addresses[m.reg_read(UC_X86_REG_ECX)],get(s+4),get(s+8)]);ret(8)
  elif address==0xae0280:ret(12)
  elif address==0xadf080:m.reg_write(UC_X86_REG_EIP,stub+16)
 m.hook_add(UC_HOOK_CODE,hook);frames=[]
 for frame,dt in enumerate(case['steps']):
  if 'weights' in case:
   for address,index in addresses.items():m.mem_write(address+0x38,struct.pack('<f',case['weights'][frame][index]));m.mem_write(address+0x8c,struct.pack('<f',case['weights'][frame][index]))
  ranges.clear();put(sp,stop);put(sp+4,dt);m.reg_write(UC_X86_REG_ESP,sp);m.reg_write(UC_X86_REG_ECX,obj);m.reg_write(UC_X86_REG_FPCW,0x027f)
  m.emu_start(0xadd670,stop,count=100000000)
  frames.append(list(ranges))
 out.append(frames)
print(json.dumps(out))
