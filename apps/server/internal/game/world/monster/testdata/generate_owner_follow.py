# ===========================================================================
# generate_owner_follow.py - original x86 steering and formation fixtures
#
# Requires pefile and Unicorn. Run with the licensed SR_GameServer.exe path.
# Geometry, timer and command boundaries are injected; native vector math,
# comparison branches, surface retries and rounding execute original bytes.
# Every execution must reach the return sentinel; an instruction cap is failure.
# ===========================================================================

"""Execute original 549F80 steering; inject timer, formation goal and command sinks."""
import argparse, hashlib, json, math, struct
from pathlib import Path
import pefile
from unicorn import Uc, UC_ARCH_X86, UC_MODE_32, UC_HOOK_CODE
from unicorn.x86_const import *
parser = argparse.ArgumentParser(description='Execute bounded owner-follow native fixtures')
parser.add_argument('executable', type=Path)
args = parser.parse_args()
exe = args.executable
output = Path(__file__).resolve().parent
executable_sha256 = hashlib.sha256(exe.read_bytes()).hexdigest()
pe = pefile.PE(str(exe))
image = pe.get_memory_mapped_image()
base = pe.OPTIONAL_HEADER.ImageBase
u = Uc(UC_ARCH_X86, UC_MODE_32)
u.mem_map(base, len(image) + 4095 & ~4095)
u.mem_write(base, image)
u.mem_map(33554432, 65536)
u.mem_map(50331648, 65536)
u.mem_map(67108864, 4096)
T, A, O, V = (33554432, 33558528, 33570816, 33587200)
STOP = 67108864
SP = 50393088
pack = lambda x: struct.pack('<I', x)

# ================
# put
# ================
def put(a, x):
	u.mem_write(a, pack(x))

# ================
# get
# ================
def get(a):
	return struct.unpack('<I', u.mem_read(a, 4))[0]

# ================
# floats
# ================
def floats(a, *x):
	u.mem_write(a, struct.pack('<' + 'f' * len(x), *x))

# ================
# pos
# ================
def pos(a, x, z):
	u.mem_write(a, struct.pack('<IIH2xfff', 0, 0, 25256, x, 0, z))

# ================
# ret
# ================
def ret(n=0, value=0):
	sp = u.reg_read(UC_X86_REG_ESP)
	u.reg_write(UC_X86_REG_EIP, get(sp))
	u.reg_write(UC_X86_REG_ESP, sp + 4 + n)
	u.reg_write(UC_X86_REG_EAX, value)
result = {}
case = {}

# ================
# hook
# ================
def hook(uc, a, size, data):
	if a == STOP:
		uc.emu_stop()
		return
	if a == 5494496:
		ret(value=1)
	elif a == 5511200:
		ret(4)
	elif a == 5592160:
		ret(4)
	elif a == 5592240:
		ret(4)
	elif a == 5628048:
		pos(uc.reg_read(UC_X86_REG_EBX), *case['slot'])
		ret(value=1)
	elif a == 5510960:
		sp = uc.reg_read(UC_X86_REG_ESP)
		p = get(sp + 4)
		result['motion'] = list(struct.unpack('<fff', uc.mem_read(p, 12))) + [struct.unpack('<f', uc.mem_read(sp + 8, 4))[0]]
		ret(8)
	elif a == V:
		ret(value=int(case['moving']))
u.mem_write(V, b'\xc3')
put(V + 1216, V)
hook_id = u.hook_add(UC_HOOK_CODE, hook)
inputs = []
for distance in [59.999, 60, 60.001, 100, 1000, 10000]:
	for angle in [0, 1, 4.999, 5, 5.001, 90, 179]:
		for moving in [False, True]:
			for remaining in [0, 30, 31, 200]:
				inputs.append( {'owner': [distance, 0], 'slot': [distance * math.cos(math.radians(angle)), distance * math.sin(math.radians(angle))], 'old': [31, 0], 'ownerGoal': [distance + remaining, 0], 'moving': moving, 'ownerMoving': remaining != 0})
for distance in [0, 29.999, 30, 30.001, 100, 10000]:
	for angle in [0, 4.999, 5, 90]:
		for heading in [0, 12345]:
			inputs.append({
				'owner': [500, 0],
				'slot': [distance * math.cos(math.radians(angle)), distance * math.sin(math.radians(angle))],
				'old': [30, 0], 'ownerGoal': [500, 0], 'moving': True, 'ownerMoving': False,
				'heading': heading,
			})
cases = []
for case in inputs:
	u.mem_write(T, b'\x00' * 32768)
	put(T + 4, A)
	put(A, V)
	put(A + 7384, O)
	put(T + 228, 0)
	floats(A + 368, 100)
	angle = case.get('heading', 0) / 65535 * 2 * math.pi
	floats(A + 36, math.cos(angle), 0, math.sin(angle))
	pos(A + 124, 0, 0)
	pos(O + 124, *case['owner'])
	for obj, goal, active in [(A, case['old'], case['moving']), (O, case['ownerGoal'], case['ownerMoving'])]:
		u.mem_write(obj + 340, struct.pack('<BBHiii', int(active), 1, 25256, int(goal[0]), 0, int(goal[1])))
	case['ownerGoal'] = [int(v) for v in case['ownerGoal']]
	for r in [UC_X86_REG_EAX, UC_X86_REG_EBX, UC_X86_REG_EDX, UC_X86_REG_ESI, UC_X86_REG_EDI, UC_X86_REG_EBP]:
		u.reg_write(r, 0)
	u.reg_write(UC_X86_REG_ESP, SP)
	u.reg_write(UC_X86_REG_ECX, T)
	u.reg_write(UC_X86_REG_FPCW, 895)
	u.reg_write(UC_X86_REG_FPSW, 0)
	u.reg_write(UC_X86_REG_FPTAG, 65535)
	put(SP, STOP)
	put(SP + 4, O)
	put(SP + 8, 0)
	result = {}
	try:
		u.emu_start(5545856, STOP, count=100000)
	except Exception as e:
		raise RuntimeError(hex(u.reg_read(UC_X86_REG_EIP))) from e
	if u.reg_read(UC_X86_REG_EIP) != STOP:
		raise RuntimeError('did not return')
	case['result'] = u.reg_read(UC_X86_REG_EAX)
	case.update(result)
	cases.append(case)
out = {'executable_sha256': executable_sha256, 'native': 'SR_GameServer 549F80; original vector math and CRT; injected timer/slot goal/command sinks', 'cases': cases}
(output / 'owner_follow_native.json').write_text(json.dumps(out, separators=(',', ':')) + '\n', encoding='utf-8', newline='\n')
print('executed', len(cases))
u.hook_del(hook_id)
G, R, D, GOAL = (33595392, 33595648, 33599488, 33603584)
SURFACE = 67109120
REACH = 67109152
SCALAR = 33607680
put(T, V)
put(V + 100, REACH)
u.mem_write(REACH, b'\xd9\x05' + pack(SCALAR) + b'\xc3')
put(14068076, G)
put(G + 60, D)
put(13383804, R)
put(R, V)
put(V + 44, SURFACE)
f32 = lambda x: struct.unpack('<f', struct.pack('<f', x))[0]
for slot in range(8):
	angle = f32((22 + 45 * slot) * 0.01745329238474369)
	x, z = (f32(math.cos(angle)), -f32(math.sin(angle)))
	length = f32(math.sqrt(f32(x * x + z * z)))
	floats(D + (slot * 3 + 27) * 4, f32(x / length), 0, f32(z / length))
probes = 0
refuse = 0
height = 0

# ================
# surface_hook
# ================
def surface_hook(uc, a, size, data):
	global probes
	if a == STOP:
		uc.emu_stop()
	elif a == SURFACE:
		probes += 1
		sp = uc.reg_read(UC_X86_REG_ESP)
		point = get(sp + 4)
		if probes > refuse:
			floats(point + 16, height)
		ret(8, value=int(probes > refuse))
u.hook_add(UC_HOOK_CODE, surface_hook)
surface_cases = []
for slot in range(-1, 8):
	for radius in [0, 1, 10, 17]:
		for refuse in [0, 3, 50]:
			for height in [0, 10]:
				for x, z in [(100, 100), (-10.5, -30.25)]:
					floats(SCALAR, radius * 3)
					pos(O + 124, x, z)
					probes = 0
					u.reg_write(UC_X86_REG_ECX, T)
					u.reg_write(UC_X86_REG_EAX, slot & 4294967295)
					u.reg_write(UC_X86_REG_EBX, GOAL)
					u.reg_write(UC_X86_REG_ESP, SP)
					u.reg_write(UC_X86_REG_FPCW, 895)
					u.reg_write(UC_X86_REG_FPSW, 0)
					u.reg_write(UC_X86_REG_FPTAG, 65535)
					put(SP, STOP)
					put(SP + 4, O)
					u.emu_start(5628048, STOP, count=100000)
					if u.reg_read(UC_X86_REG_EIP) != STOP:
						raise RuntimeError('surface did not return')
					region = struct.unpack('<H', u.mem_read(GOAL + 8, 2))[0]
					goal = list(struct.unpack('<fff', u.mem_read(GOAL + 12, 12)))
					surface_cases.append(dict(slot=slot, radius=radius, refuse=refuse, height=height, owner=[x, 0, z], region=region, goal=goal, probes=probes))
(output / 'owner_follow_surface_native.json').write_text(json.dumps({'executable_sha256': executable_sha256, 'native': '55E090 with native direction floats; only virtual reach and surface response injected', 'cases': surface_cases}, separators=(',', ':')) + '\n', encoding='utf-8', newline='\n')
print('surface', len(surface_cases))
