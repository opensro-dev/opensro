"""
===========================================================================

native-action-entry.py - original one-shot entry and completion clocks

Runs AE00C0/ADFFB0 reset, ADF310 transition setup and AE07E0 advancement from
licensed v1.150 bytes. Only key/sound receivers and root motion are isolated;
the cursor, blend, completion and reset implementations execute unchanged.

===========================================================================
"""
import hashlib
import json
from pathlib import Path
import struct
import sys

import pefile
from unicorn import Uc, UC_ARCH_X86, UC_MODE_32, UC_HOOK_CODE
from unicorn.x86_const import (
    UC_X86_REG_ESP, UC_X86_REG_ECX, UC_X86_REG_EAX, UC_X86_REG_EIP,
    UC_X86_REG_FPCW,
)

sys.path.insert(0, str(Path(__file__).resolve().parents[3] / "scripts"))
from sro_paths import GAME_ROOT

CLIENT_SHA256 = "375e868234437e815af8ce9289ddea7ec9144430f4ea24e32988a6d6c9dd108a"
ARENA = 0x10000000
RESET = 0xAE00C0
TRANSITIONS = 0xADF310
ADVANCE = 0xAE07E0
GET_CURSOR = 0xADF3A0
SET_RATE = 0xADF030


# ================
# capture
# ================
def capture(pe, case):
    machine = Uc(UC_ARCH_X86, UC_MODE_32)
    base = pe.OPTIONAL_HEADER.ImageBase
    machine.mem_map(base, (pe.OPTIONAL_HEADER.SizeOfImage + 4095) & ~4095)
    machine.mem_write(base, pe.get_memory_mapped_image())
    machine.mem_map(ARENA, 0x40000)
    obj, clip, vtable = ARENA + 0x1000, ARENA + 0x2000, ARENA + 0x3000
    stack, stop, output = ARENA + 0x30000, ARENA + 0x3F000, ARENA + 0x4000
    motion_stub = ARENA + 0x5000
    machine.mem_write(motion_stub, b"\xd9\xee\xc2\x08\x00")
    ranges = []

    # ================
    # put
    # ================
    def put(address, value):
        machine.mem_write(address, struct.pack("<I", value))

    # ================
    # get
    # ================
    def get(address):
        return struct.unpack("<I", machine.mem_read(address, 4))[0]

    # ================
    # return_from_receiver
    # ================
    def return_from_receiver(size):
        at = machine.reg_read(UC_X86_REG_ESP)
        machine.reg_write(UC_X86_REG_ESP, at + 4 + size)
        machine.reg_write(UC_X86_REG_EIP, get(at))

    # ================
    # receiver_boundary
    # ================
    def receiver_boundary(unused_machine, address, unused_size, unused_data):
        at = machine.reg_read(UC_X86_REG_ESP)
        if address == 0xAE0710:
            ranges.append([get(at + 4), get(at + 8)])
            return_from_receiver(8)
        elif address == 0xAE0280:
            return_from_receiver(12)
        elif address == 0xADF080:
            machine.reg_write(UC_X86_REG_EIP, motion_stub)
        elif address == 0xAE0200:
            return_from_receiver(0)

    # ================
    # call
    # ================
    def call(address, args=()):
        put(stack, stop)
        for index, arg in enumerate(args):
            put(stack + 4 + index * 4, arg)
        machine.reg_write(UC_X86_REG_ESP, stack)
        machine.reg_write(UC_X86_REG_ECX, obj)
        machine.reg_write(UC_X86_REG_FPCW, 0x027F)
        machine.emu_start(address, stop, count=100000)
        if machine.reg_read(UC_X86_REG_EIP) != stop:
            raise RuntimeError("Native execution did not return")
        return machine.reg_read(UC_X86_REG_EAX)

    machine.hook_add(UC_HOOK_CODE, receiver_boundary)
    put(obj, vtable)
    put(obj + 8, clip)
    put(clip + 0x58, case["durationMs"])
    put(clip + 0xA0, 1)
    put(vtable + 0xC, GET_CURSOR)
    call(RESET)
    call(TRANSITIONS, (case["enterMs"], case["exitMs"]))
    call(SET_RATE, (struct.unpack("<I", struct.pack("<f", case.get("rate", 1)))[0],))
    elapsed = 0
    frames = []
    for delta in case["steps"]:
        ranges.clear()
        flags = call(ADVANCE, (delta, output, 0))
        elapsed += delta
        frames.append({
            "elapsedMs": elapsed, "cursorMs": get(obj + 0x2C),
            "sampleMs": call(GET_CURSOR), "mode": get(obj + 0x10),
            "weight": struct.unpack("<f", machine.mem_read(obj + 0x38, 4))[0],
            "flags": flags, "ranges": list(ranges),
        })
    return {**case, "frames": frames}


# ================
# main
# ================
def main():
    raw = (GAME_ROOT / "SRO_Client.exe").read_bytes()
    if hashlib.sha256(raw).hexdigest() != CLIENT_SHA256:
        raise RuntimeError("Wrong native client")
    pe = pefile.PE(data=raw)
    cases = json.load(sys.stdin)
    print(json.dumps({"sha256": CLIENT_SHA256,
                      "cases": [capture(pe, case) for case in cases]}, indent=2))


if __name__ == "__main__":
    main()
