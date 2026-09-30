"""
===========================================================================

native-modifier-keys.py - Execute original AE0380/ADF890 with a bounded authored key tree

SRO_Client.exe is read from the game root (scripts/sro_paths.py).

===========================================================================
"""
import hashlib,json,struct,sys
from pathlib import Path
sys.path.insert(0, str(Path(__file__).resolve().parents[3] / 'scripts'))
from sro_paths import GAME_ROOT  # noqa: E402  (SRO_GAME_ROOT or beside the main checkout)
import pefile
from unicorn import Uc,UC_ARCH_X86,UC_MODE_32,UC_HOOK_CODE
from unicorn.x86_const import UC_X86_REG_ESP,UC_X86_REG_ECX,UC_X86_REG_EIP
raw=(GAME_ROOT/'SRO_Client.exe').read_bytes()
assert hashlib.sha256(raw).hexdigest()=='375e868234437e815af8ce9289ddea7ec9144430f4ea24e32988a6d6c9dd108a'
pe=pefile.PE(data=raw);base=pe.OPTIONAL_HEADER.ImageBase;out=[]
for case in json.load(sys.stdin):
 m=Uc(UC_ARCH_X86,UC_MODE_32);m.mem_map(base,(pe.OPTIONAL_HEADER.SizeOfImage+4095)&~4095);m.mem_write(base,pe.get_memory_mapped_image())
 arena=0x10000000;m.mem_map(arena,0x10000);obj=arena+0x1000;head=arena+0x2000;owner=arena+0x3000;vt=owner+0x100;stub=vt+0x100;sp=arena+0xf000;stop=sp+0x100
 def put(at,n):m.mem_write(at,struct.pack('<I',n))
 def get(at):return struct.unpack('<I',m.mem_read(at,4))[0]
 put(obj+0x40,1);put(obj+0x4c,head);put(owner,vt);put(vt+0x24,stub);m.mem_write(head+0x19,b'\x01')
 keys=sorted(enumerate(case['keys']),key=lambda x:x[1]);nodes=[arena+0x4000+i*0x40 for i in range(len(keys))]
 def tree(lo,hi,parent):
  if(lo>=hi):return head
  i=(lo+hi)//2;n=nodes[i];put(n,tree(lo,i,n));put(n+4,parent);put(n+8,tree(i+1,hi,n));put(n+0xc,keys[i][1]);put(n+0x10,owner);put(n+0x14,keys[i][0]);return n
 put(head+4,tree(0,len(keys),head));put(head,nodes[0] if nodes else head);put(head+8,nodes[-1] if nodes else head)
 hits=[]
 def hook(machine,address,size,user):
  if address==stub:
   s=m.reg_read(UC_X86_REG_ESP);hits.append(get(s+4));m.reg_write(UC_X86_REG_ESP,s+8);m.reg_write(UC_X86_REG_EIP,get(s))
 m.hook_add(UC_HOOK_CODE,hook)
 for start,end in case['ranges']:
  put(sp,stop);put(sp+4,start);put(sp+8,end);m.reg_write(UC_X86_REG_ESP,sp);m.reg_write(UC_X86_REG_ECX,obj);m.emu_start(0xae0380,stop,count=100000)
 out.append(hits)
print(json.dumps(out))
