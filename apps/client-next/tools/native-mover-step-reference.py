"""
===========================================================================

native-mover-step-reference.py - original server elapsed-time movement arithmetic

Executes complete 48C1B0 and 48BFF0 without substituted instructions or calls.
The separate 48C5AE..48C63F suffix proves destination-vector clamping only;
it does not certify destination resolution, turning or world traversal.

Usage: python SCRIPT SR_GameServer.exe OUTPUT_JSON

===========================================================================
"""
import hashlib
import json
import struct
import sys
from pathlib import Path

import pefile
from unicorn import Uc, UC_ARCH_X86, UC_MODE_32
from unicorn.x86_const import (
	UC_X86_REG_EAX, UC_X86_REG_EBX, UC_X86_REG_ECX,
	UC_X86_REG_EIP, UC_X86_REG_ESP, UC_X86_REG_FPCW,
)

BINARY_SHA256 = "bec2375e2c4c1073e3bf7761571470c430de251de74b452dbb86537348ef5290"
SCRATCH = 0x20000000
STACK = SCRATCH + 0x8000
STOP = SCRATCH + 0xF000
STEP = SCRATCH + 0x1000
MOVER = SCRATCH + 0x100
ACTOR = SCRATCH + 0x200
INSTRUCTION_BUDGET = 10000
FPU_CONTROL_WORD = 0x037F


# ================
# f32
# ================
def f32(value):
	return struct.unpack("<f", struct.pack("<f", value))[0]


# ================
# words
# ================
def words(machine, address, *values):
	machine.mem_write(address, struct.pack("<" + "I" * len(values), *values))


# ================
# floats
# ================
def floats(machine, address, *values):
	machine.mem_write(address, struct.pack("<" + "f" * len(values), *values))


# ================
# run_interval
# A budget cutoff is a failure, never a truncated successful oracle.
# ================
def run_interval(machine, start, stop):
	machine.emu_start(start, stop, count=INSTRUCTION_BUDGET)
	if machine.reg_read(UC_X86_REG_EIP) != stop:
		raise RuntimeError("Native movement interval exceeded its instruction budget")


# ================
# read_step
# ================
def read_step(machine):
	data = machine.mem_read(STEP, 12)
	return {
		"step": list(struct.unpack("<3f", data)),
		"stepBits": list(struct.unpack("<3I", data)),
	}


# ================
# generate
# ================
def generate(executable):
	raw = Path(executable).read_bytes()
	if hashlib.sha256(raw).hexdigest() != BINARY_SHA256:
		raise ValueError("Unexpected server executable")
	pe = pefile.PE(data=raw)
	image = pe.get_memory_mapped_image()
	base = pe.OPTIONAL_HEADER.ImageBase

	# ================
	# machine
	# Only data fixtures are synthetic. Every executed instruction is retail.
	# ================
	def machine():
		instance = Uc(UC_ARCH_X86, UC_MODE_32)
		instance.mem_map(base, (len(image) + 4095) & ~4095)
		instance.mem_write(base, image)
		instance.mem_map(SCRATCH, 0x10000)
		instance.reg_write(UC_X86_REG_FPCW, FPU_CONTROL_WORD)
		return instance

	# The case matrix is fixed independently of native results. Irregular and
	# stalled elapsed values distinguish actual elapsed time from fixed steps.
	inputs = []
	for speed in [20, 50, 95.75]:
		for elapsed in [0, .001, .016, .033, .1, .15, .3, 1, 4]:
			for direction in [(1, 0), (.6, -.8)]:
				inputs.append((f"speed-{speed}-time-{elapsed}-direction-{direction}", speed, elapsed, direction))
	inputs.extend([
		("component-below", 1, .009999998, (1, -1)),
		("component-equal", 1, f32(.01), (1, -1)),
		("component-above", 1, .010000001, (1, -1)),
		("one-component-suppressed", 1, .1, (.09, 1)),
		("negative-speed", -50, .033, (-.6, .8)),
		("negative-time", 50, -.033, (.6, -.8)),
		("cap-below", 160, .99999994, (1, 0)),
		("cap-equal", 160, 1, (1, 0)),
		("cap-above", 160, 1.000000119, (1, 0)),
		("rounded-product", .333333343, .0300000012, (1, 0)),
	])
	rows = []
	for name, speed, elapsed, direction in inputs:
		instance = machine()
		words(instance, MOVER + 4, ACTOR)
		words(instance, MOVER + 0xC, 1)
		floats(instance, ACTOR + 0x174, speed)
		floats(instance, ACTOR + 0x24, direction[0], 7, direction[1])
		words(instance, STACK, STOP)
		floats(instance, STACK + 4, elapsed)
		words(instance, STACK + 8, STEP)
		floats(instance, STEP, 17, 19, 23)
		instance.reg_write(UC_X86_REG_ECX, MOVER)
		instance.reg_write(UC_X86_REG_ESP, STACK)
		run_interval(instance, 0x48C1B0, STOP)
		rows.append({
			"name": name, "speed": f32(speed), "elapsedSeconds": f32(elapsed),
			"direction": list(map(f32, direction)),
			"accepted": instance.reg_read(UC_X86_REG_EAX) == 1, **read_step(instance),
		})

	inactive = []
	for state in [0, 2]:
		instance = machine()
		words(instance, MOVER + 0xC, state)
		words(instance, STACK, STOP)
		floats(instance, STACK + 4, .3)
		words(instance, STACK + 8, STEP)
		floats(instance, STEP, 17, 19, 23)
		instance.reg_write(UC_X86_REG_ECX, MOVER)
		instance.reg_write(UC_X86_REG_ESP, STACK)
		run_interval(instance, 0x48C1B0, STOP)
		inactive.append({"state": state, "accepted": instance.reg_read(UC_X86_REG_EAX) == 1, **read_step(instance)})

	clamps = []
	for step, remaining in [
		([1, 0, 0], [1, 0, 0]),
		([1, 0, 0], [.99999994, 0, 0]),
		([1, 0, 0], [1.000000119, 0, 0]),
		([.6, 0, .8], [.3, 0, .4]),
		([-10, 0, 0], [-3, 4, 0]),
		([1, 0, 0], [0, 1, 0]),
		([0, 0, 0], [0, 0, 0]),
		([0, 0, 0], [0, 1, 0]),
		([160, 0, 0], [159, 0, 0]),
	]:
		instance = machine()
		floats(instance, STEP, *step)
		# Enter just after Pos_GetRelativePlanar returned, before its remaining
		# stack argument is popped at 48C5B3. Its output is three float locals.
		floats(instance, STACK + 0x34, *remaining)
		instance.reg_write(UC_X86_REG_EBX, STEP)
		instance.reg_write(UC_X86_REG_ESP, STACK)
		run_interval(instance, 0x48C5AE, 0x48C63F)
		clamps.append({"requestedStep": list(map(f32, step)), "remaining": list(map(f32, remaining)), **read_step(instance)})

	return {
		"format": "sro-native-mover-step-v1", "binarySha256": BINARY_SHA256,
		"generatorSha256": hashlib.sha256(Path(__file__).read_bytes()).hexdigest(),
		"floatingControlWord": hex(FPU_CONTROL_WORD),
		"intervals": ["0x48c1b0..0x48c1d6", "0x48bff0..0x48c0a7", "0x48c5ae..0x48c63f"],
		"scope": "Original command-mover state gate and step arithmetic; separate bounded destination-clamp suffix with original square-root calls. No instruction/call substitution. No proof of turning, elapsed-time sourcing, destination resolution or collision traversal.",
		"cases": rows, "inactiveCases": inactive, "clampCases": clamps,
	}


if __name__ == "__main__":
	Path(sys.argv[2]).write_text(json.dumps(generate(sys.argv[1]), indent=2) + "\n", encoding="utf-8", newline="\n")
