"""
===========================================================================
capture_trade_rewards.py - execute native job EXP and weekly reward arithmetic

Reuses the pinned PE mapping. The tested arithmetic is never replaced.
External role/tier queries and logging are explicit fixture dependencies.
===========================================================================
"""
import argparse
import json
import pathlib
import struct

from capture_trade_quotation import NativeQuotation, SERVER_SHA256, ENTRY, RECORD, VTABLE, STACK, RETURN
from unicorn import UC_HOOK_CODE
from unicorn.x86_const import UC_X86_REG_EAX, UC_X86_REG_EIP, UC_X86_REG_ESP


# ================
# capture
# ================
def capture(native):
	cpu = native.cpu
	log_displacement = struct.unpack("<i", cpu.mem_read(0x60E157, 4))[0]
	log_address = 0x60E15B + log_displacement
	role_query = RETURN + 16
	fixture = {"role": 1, "tier": 1}

	# ================
	# dependency
	# ================
	def dependency(cpu, address, size, data):
		if address not in (role_query, 0x5237A0, log_address):
			return
		stack = cpu.reg_read(UC_X86_REG_ESP)
		back = struct.unpack("<I", cpu.mem_read(stack, 4))[0]
		if address != log_address:
			cpu.reg_write(UC_X86_REG_EAX, fixture["role" if address == role_query else "tier"])
		cpu.reg_write(UC_X86_REG_ESP, stack + 4)
		cpu.reg_write(UC_X86_REG_EIP, back)

	cpu.hook_add(UC_HOOK_CODE, dependency)
	weekly = []
	for current in (-2147483648, -1, 0, 1, 100, 1999999999, 2000000000, 2000000001, 2147483647):
		for delta in (-2147483648, -2000000000, -101, -1, 0, 1, 99, 2147483647):
			cpu.context_restore(native.initial)
			cpu.mem_write(ENTRY + 4, struct.pack("<I", RECORD))
			cpu.mem_write(RECORD + 0x24, struct.pack("<i", current))
			cpu.mem_write(0xD21E1C, b"\x01")
			cpu.mem_write(STACK, struct.pack("<IIi", RETURN, ENTRY, delta))
			cpu.reg_write(UC_X86_REG_ESP, STACK)
			native.run(0x60E0A0, RETURN)
			value = struct.unpack("<i", cpu.mem_read(RECORD + 0x24, 4))[0]
			weekly.append({"current": current, "delta": delta, "value": value})
	experience = []
	for role in (0, 1, 2, 3, 4):
		for tier in (1, 2):
			for profit in (-1, 0, 1, 2, 3, 13, 101, 303, 10001, 16777217, 2000000000):
				cpu.context_restore(native.initial)
				fixture.update(role=role, tier=tier)
				cpu.mem_write(ENTRY, struct.pack("<I", VTABLE))
				cpu.mem_write(VTABLE + 0x11C, struct.pack("<I", role_query))
				cpu.mem_write(STACK, struct.pack("<Iq", RETURN, profit))
				cpu.reg_write(UC_X86_REG_EAX, ENTRY)
				cpu.reg_write(UC_X86_REG_ESP, STACK)
				native.run(0x410520, RETURN)
				experience.append({"job": role, "oneStar": tier == 1, "profit": profit, "value": cpu.reg_read(UC_X86_REG_EAX)})
	return {"generator": "scripts/tools/capture_trade_rewards.py", "sourceSHA256": SERVER_SHA256,
		"precision": 53, "weekly": weekly, "experience": experience}


# ================
# main
# ================
def main():
	parser = argparse.ArgumentParser(description=__doc__)
	parser.add_argument("server", type=pathlib.Path)
	parser.add_argument("output", type=pathlib.Path)
	args = parser.parse_args()
	document = capture(NativeQuotation(args.server))
	args.output.write_text(json.dumps(document, indent="\t") + "\n", encoding="utf-8", newline="\n")
	print("Captured", len(document["weekly"]), "weekly and", len(document["experience"]), "EXP cases")


if __name__ == "__main__":
	main()
