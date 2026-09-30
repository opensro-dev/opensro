"""
===========================================================================

native-deferred-alpha.py - Execute original AEC0D0. No fader arithmetic is substituted

SRO_Client.exe is read from the game root (scripts/sro_paths.py).

===========================================================================
"""
import json,struct,sys,hashlib
from pathlib import Path
sys.path.insert(0, str(Path(__file__).resolve().parents[3] / 'scripts'))
from sro_paths import GAME_ROOT  # noqa: E402  (SRO_GAME_ROOT or beside the main checkout)
import pefile
from unicorn import Uc,UC_ARCH_X86,UC_MODE_32,UC_HOOK_CODE
from unicorn.x86_const import UC_X86_REG_ESP,UC_X86_REG_ECX,UC_X86_REG_FPCW,UC_X86_REG_EAX,UC_X86_REG_EIP
root=Path(__file__).resolve().parents[1]
raw=(GAME_ROOT/'SRO_Client.exe').read_bytes()
assert hashlib.sha256(raw).hexdigest()=='375e868234437e815af8ce9289ddea7ec9144430f4ea24e32988a6d6c9dd108a'
pe=pefile.PE(data=raw);base=pe.OPTIONAL_HEADER.ImageBase
m=Uc(UC_ARCH_X86,UC_MODE_32);m.mem_map(base,(pe.OPTIONAL_HEADER.SizeOfImage+4095)&~4095);m.mem_write(base,pe.get_memory_mapped_image())
arena=0x10000000;m.mem_map(arena,0x10000);obj=arena+0x1000;sp=arena+0x8000;stop=arena+0xf000
write=lambda at,v:m.mem_write(at,struct.pack('<I',v))
read=lambda at:struct.unpack('<I',m.mem_read(at,4))[0]
out=[]
calls=[]
# ================
# hook
#
# Unicorn code hook: with --routes, stub the boundaries the fader calls (the
# allocator, tick and deferred receivers) and record the calls it makes.
# ================
def hook(machine,at,size,user):
 if '--routes' not in sys.argv:return
 result=None;cleanup=0
 if at==0xa2e560:result=arena+0x2000
 elif at==0xa2e520:result=arena+0x3000
 elif at==0xaec1f0:pass
 elif at==0xaec040:calls.append('tick')
 elif at==arena+0x6000:calls.append('deferred');cleanup=4
 elif at==arena+0x6010:calls.append('draw')
 else:return
 stack=m.reg_read(UC_X86_REG_ESP);ret=read(stack);m.reg_write(UC_X86_REG_ESP,stack+4+cleanup)
 if result is not None:m.reg_write(UC_X86_REG_EAX,result)
 m.reg_write(UC_X86_REG_EIP,ret)
m.hook_add(UC_HOOK_CODE,hook)
for case in json.load(sys.stdin):
 m.reg_write(UC_X86_REG_FPCW,0x027f)
 if '--routes' in sys.argv:
  calls.clear();m.mem_write(obj,bytes(0x100));write(obj+0xb0,1);write(obj+0x54,arena+0x4000)
  write(arena+0x3000,arena+0x5000);write(arena+0x5034,arena+0x6000)
  write(arena+0x4000,arena+0x5100);write(arena+0x5104,arena+0x6010)
  write(0xf10278,1);write(0xf10274,int(case['supported']));write(0xf1026c,int(case['enabled']));write(0xf100c8+0x1a8,int(case['night']))
  m.mem_write(obj+0x90,bytes([case['offset'],int(case['nightOnly'])]))
  write(sp,stop);m.reg_write(UC_X86_REG_ECX,obj);m.reg_write(UC_X86_REG_ESP,sp);m.emu_start(0xaec4b0,stop,count=10000)
  assert m.reg_read(UC_X86_REG_ESP)==sp+4
  assert calls in [[],['tick','draw'],['deferred']],calls
  out.append('hidden' if not calls else 'deferred' if calls==['deferred'] else 'immediate');continue
 if '--points' in sys.argv:
  m.mem_write(obj,struct.pack('<3f',*case));write(sp,stop);m.reg_write(UC_X86_REG_ECX,obj);m.reg_write(UC_X86_REG_ESP,sp);m.emu_start(0x410890,stop,count=10000);out.append(list(struct.unpack('<3f',m.mem_read(obj,12))));continue
 m.mem_write(obj,bytes(0x100));m.mem_write(obj+0x4c,bytes([case['alpha']]))
 write(obj+0xb8,case['last']);write(obj+0xbc,int(case['visible']));write(0xf100c8+0x188,case['frame']);write(0xf100c8+0x1d8,case['delta'])
 write(sp,stop);write(sp+4,int(case['requested']));m.reg_write(UC_X86_REG_ECX,obj);m.reg_write(UC_X86_REG_ESP,sp)
 m.emu_start(0xaec0d0,stop,count=10000)
 out.append({'alpha':m.mem_read(obj+0x4c,1)[0],'visible':bool(read(obj+0xbc)),'last':read(obj+0xb8)})
print(json.dumps(out))
