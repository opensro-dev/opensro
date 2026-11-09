"""
===========================================================================
revtool_break.py - cmd_runtime and cmd_break: live reads and hardware breakpoints

Part of revtool, the non-destructive symbol overlay for the SRO_Client
pseudocode dump. revtool.py is the entry point; this module owns one
command group or layer and is imported by it (and by revtool_rizin.py
through revtool's re-exports). The dump is an immutable Binary Ninja
hypothesis snapshot: original PE bytes and exact proofs outrank it,
and nothing here edits it.
===========================================================================
"""

import os
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

from revtool_d3d import IMAGE_BASE_DEFAULT
from revtool_dump import get_binary
from revtool_pe import open_pe, resolve_addr
from revtool_runtime import Debugger, LiveProcess, _ctx_regs
from revtool_runtime import _print_runtime, _print_watch
from revtool_runtime import _running_pids_by_name

def cmd_runtime(args, con):
	"""Resolve a runtime-populated value by running the client and reading its memory.

    The .bss/runtime case `deref` can't see (vtable slot, current-process pointer, a
    function pointer like SWorld+0x14): launch SRO_Client.exe minimised in the
    background, poll the address until it is set & stable (that wait IS the lifecycle:
    splash -> title), resolve the value to a (possibly renamed) function, then kill it.
    """
	if sys.platform != "win32":
		print("error: `runtime` is Windows-only (uses ReadProcessMemory)", file=sys.stderr)
		return 1
	import struct
	import time
	exe = args.exe or get_binary(con, args)
	modname = os.path.basename(exe).lower()

	# `--list-clients`: pure discovery. Snapshot running processes by image name and print
	# their PIDs so the caller can pick a `--pid` to attach to. Never launches/attaches/kills,
	# so it can never hang on warmup or crash a live client.
	if getattr(args, "list_clients", False):
		pids = _running_pids_by_name(modname)
		if not pids:
			print(f"no running {modname} (nothing to attach to).")
		else:
			print(f"running {modname}: {len(pids)} instance(s)")
			for p in pids:
				print(f"  pid {p}   ->  read in place:  revtool.py runtime <addr> --pid {p}")
		return 0

	if args.addr is None:
		print("error: addr is required (or pass --list-clients to just list running clients)",
			  file=sys.stderr)
		return 1
	try:
		base_va = int(args.addr, 16)
	except ValueError:
		print(f"error: bad address {args.addr!r}", file=sys.stderr)
		return 1
	count = max(1, args.count)

	# `--expect code` waits for the first dword to be a real pointer INTO the module
	# (a function pointer), not an early-init transient / heap value. Needs the PE to
	# know the module's mapped range.
	in_image = None
	if args.expect == "code":
		pe = open_pe(con, args)
		if pe is None:
			return 1
		in_image = pe.in_image

	def settled(words):
		first = words[0]
		if args.expect == "code":
			return in_image(first)
		if args.expect == "nonzero":
			return first != 0
		return True  # any

	if args.pid:
		try:
			lp = LiveProcess.attach(args.pid)
		except OSError as e:
			print(f"error: {e}", file=sys.stderr)
			return 1
		owned = False
		print(f"attached to pid {args.pid} ({modname})")
	else:
		if not os.path.exists(exe):
			print(f"error: exe not found: {exe}\n"
				  f"  set it once: revtool config binary <path>, or pass --exe", file=sys.stderr)
			return 1
		# A second instance of the client crashes at splash (the first holds a
		# single-instance lock / shared resource), which previously surfaced as the
		# confusing "exited during warmup". Detect an already-running client up front
		# and refuse to launch a doomed second one. We do NOT kill it - the user may be
		# using it - we just tell them how to proceed.
		existing = _running_pids_by_name(modname)
		if existing:
			pid_list = ", ".join(str(p) for p in existing)
			print(f"error: {modname} is ALREADY running (pid {pid_list}).\n"
				  f"  Launching a second instance crashes at splash, so revtool will not do it.\n"
				  f"  -> either close that client, then re-run; or read from it in place without\n"
				  f"     launching/killing:  revtool.py runtime {args.addr} --pid {existing[0]}",
				  file=sys.stderr)
			return 1
		try:
			lp = LiveProcess.launch(exe, args.args.split() if args.args else None)
		except OSError as e:
			print(f"error: launch failed: {e}", file=sys.stderr)
			return 1
		owned = True
		hidden = "hidden off-screen" if args.hide else "on-screen (--no-hide)"
		print(f"launched {os.path.basename(exe)} (pid {lp.pid}, {hidden}) - "
			  f"{'watching' if args.watch else 'polling'} 0x{base_va:08x} up to {args.timeout}s ...")

	try:
		hide_on = owned and args.hide

		def tame():
			if hide_on:
				try:
					lp.move_offscreen()
				except OSError:
					pass

		if hide_on:
			# catch the window the instant it appears and shove it off-screen, so it barely
			# flashes (tight loop), then keep enforcing in the warmup/poll loops below.
			t_end = time.time() + 4.0
			while time.time() < t_end:
				if lp.alive() and lp.move_offscreen() > 0:
					break
				time.sleep(0.03)
		if args.warmup > 0:
			# let the client get past splash/init before the first read, so we don't
			# latch onto constructor-time junk. Bail early if it dies during warmup.
			print(f"warming up {args.warmup:.0f}s before first read ...")
			warm_end = time.time() + args.warmup
			while time.time() < warm_end:
				if owned and not lp.alive():
					print("error: the client exited during warmup (needs launch args, or "
						  "crashed at splash). Try --pid against a client you started.",
						  file=sys.stderr)
					return 1
				tame()
				# warm_end - now can go negative between the loop test and here
				time.sleep(max(0.0, min(0.5, warm_end - time.time())))

		nbytes = count * 4

		if args.watch:
			# sample over the WHOLE window and report what each offset does - removes the
			# "guess the exact instant" fragility for per-frame / per-draw values.
			modbase = None
			samples = []   # (elapsed, words)
			wstart = time.time()
			wdeadline = wstart + args.timeout
			while time.time() < wdeadline:
				if owned and not lp.alive():
					print("error: the client exited during watch.", file=sys.stderr)
					break
				if modbase is None:
					modbase = lp.module_base(modname)
				delta = (modbase - IMAGE_BASE_DEFAULT) if modbase else 0
				data = lp.read(base_va + delta, nbytes)
				if data and len(data) == nbytes:
					samples.append((time.time() - wstart, struct.unpack(f"<{count}I", data)))
				tame()
				time.sleep(args.interval / 1000.0)
			return _print_watch(con, base_va, count, samples, modbase, args)

		start = time.time()
		deadline = start + args.timeout
		modbase = None
		prev = None
		last_seen = None       # last successfully-read value, even if not 'settled'
		stable_hits = 0
		need_stable = max(1, args.samples)
		while time.time() < deadline:
			tame()
			if owned and not lp.alive():
				print("error: the client exited before the value was set "
					  "(needs launch args, or crashed at splash). Try --pid against a "
					  "client you started, or pass --args.", file=sys.stderr)
				return 1
			if modbase is None:
				modbase = lp.module_base(modname)
			delta = (modbase - IMAGE_BASE_DEFAULT) if modbase else 0
			data = lp.read(base_va + delta, nbytes)
			if data and len(data) == nbytes:
				words = struct.unpack(f"<{count}I", data)
				last_seen = words
				if settled(words):
					# require the value to hold steady across N samples so we don't
					# latch onto an early-init transient
					stable_hits = stable_hits + 1 if words == prev else 1
					prev = words
					if stable_hits >= need_stable:
						return _print_runtime(con, base_va, words, modbase,
											  time.time() - start, args.observe, args, True)
				else:
					prev = None
					stable_hits = 0
			time.sleep(args.interval / 1000.0)
		if prev:
			print(f"note: value seen but not stable for {need_stable} samples within "
				  f"{args.timeout}s; last read:")
			return _print_runtime(con, base_va, prev, modbase, time.time() - start,
								  args.observe, args, True)
		if last_seen is not None and last_seen[0] != 0:
			print(f"note: 0x{base_va:08x} was non-zero ({last_seen[0]:#010x}) but never matched "
				  f"--expect {args.expect} within {args.timeout}s.\n"
				  f"  (e.g. --expect code wants a pointer INTO the module; this value is outside "
				  f"it - a heap/object pointer, not a .text function.) Last live read:")
			return _print_runtime(con, base_va, last_seen, modbase, time.time() - start,
								  args.observe, args, True)
		print(f"timed out after {args.timeout}s: 0x{base_va:08x} still 0.\n"
			  f"  the process likely hasn't reached the lifecycle stage that sets it "
			  f"(or needs proper launch args). Increase --timeout, or attach with --pid\n"
			  f"  once the client is at the scene where the value is live.", file=sys.stderr)
		return 1
	finally:
		if owned and not args.keep:
			lp.terminate()
			print(f"terminated pid {lp.pid}")
		elif owned and args.keep:
			print(f"left pid {lp.pid} running (--keep)")
		lp.close()


def _parse_read_expr(expr):
	"""Parse a capture expression into (kind, reg, off, vtype).
      reg                 -> ('reg', reg, 0, None)            value of a register
      reg+0x90            -> ('addr', reg, 0x90, None)        computed address
      [reg+0x90]:f32      -> ('mem', reg, 0x90, 'f32')        4 bytes read at addr
    vtype in {f32,i32,u32,hex} (mem only; default hex)."""
	e = expr.strip().lower()
	vtype = None
	if ":" in e:
		e, vtype = e.rsplit(":", 1)
		e = e.strip()
		vtype = vtype.strip()
	deref = e.startswith("[") and e.endswith("]")
	if deref:
		e = e[1:-1].strip()
	if "+" in e:
		reg, offs = e.split("+", 1)
		reg = reg.strip()
		off = int(offs.strip(), 16) if offs.strip().lower().startswith("0x") else int(offs.strip())
	elif "-" in e[1:]:
		reg, offs = e[0] + e[1:].split("-", 1)[0], e[1:].split("-", 1)[1]
		reg = reg.strip()
		off = -(int(offs.strip(), 16) if offs.strip().lower().startswith("0x") else int(offs.strip()))
	else:
		reg, off = e, 0
	regs = ("eax", "ebx", "ecx", "edx", "esi", "edi", "ebp", "esp", "eip", "eflags")
	if reg not in regs:
		raise ValueError(f"unknown register {reg!r} in expr {expr!r}")
	if deref:
		return ("mem", reg, off, vtype or "hex")
	return ("addr" if off else "reg", reg, off, vtype)


def _fmt_mem(raw, vtype):
	import struct as _s
	if raw is None or len(raw) < 4:
		return "<unreadable>"
	w = _s.unpack("<I", raw[:4])[0]
	if vtype == "f32":
		f = _s.unpack("<f", raw[:4])[0]
		return f"{f:.6g}"
	if vtype == "i32":
		return str(w - 0x100000000 if w >= 0x80000000 else w)
	if vtype == "u32":
		return str(w)
	return f"0x{w:08x}"


def _eval_reads(exprs, regs, lp):
	"""Render each capture expression against a register snapshot + live reads."""
	out = []
	for raw_expr in exprs:
		kind, reg, off, vtype = _parse_read_expr(raw_expr)
		rv = regs[reg]
		if kind == "reg":
			out.append(f"{reg}=0x{rv:08x}")
		elif kind == "addr":
			out.append(f"{raw_expr}=0x{(rv + off) & 0xffffffff:08x}")
		else:  # mem
			addr = (rv + off) & 0xffffffff
			data = lp.read(addr, 4)
			out.append(f"{raw_expr}=@0x{addr:08x} {_fmt_mem(data, vtype)}")
	return out


def cmd_break(args, con):
	"""Hardware-breakpoint capture on a RUNNING client (never launches/kills it).

      exec  <VA>            trap when EIP reaches VA; dump registers (+ --read).
      write <ADDR>          trap when 4 bytes at ADDR are written; report the
                            writer instruction (EIP) + the new value.
      field <VA> <reg> <off> two-phase: exec-trap at VA, read <reg>+<off> as the
                            instance field address, then arm a WRITE breakpoint on
                            it and report the instruction that computes/stores it.
    Uses CPU debug registers (DR0-DR7) - no code patching - and detaches with
    KillOnExit=FALSE so the client keeps running afterward."""
	if sys.platform != "win32":
		print("error: `break` is Windows-only (uses the Win32 debug API)", file=sys.stderr)
		return 1
	if not args.pid:
		print("error: `break` requires --pid (it attaches to a client you already "
			  "have at the right scene; it never launches one)", file=sys.stderr)
		return 1
	if args.mode == "field" and (not args.reg or args.off is None):
		print("error: field mode needs <reg> and <off>, e.g. "
			  "`break field 0x8b1398 ecx 0x90 --pid PID`", file=sys.stderr)
		return 1
	if args.mode == "field":
		if args.reg.lower() not in ("eax", "ebx", "ecx", "edx", "esi", "edi", "ebp", "esp"):
			print(f"error: bad base register {args.reg!r}", file=sys.stderr)
			return 1
		args.reg = args.reg.lower()

	def parse_addr(s):
		return int(s, 16) if s.lower().startswith("0x") else int(s, 16)

	try:
		primary = parse_addr(args.addr)
	except ValueError:
		print(f"error: bad address {args.addr!r}", file=sys.stderr)
		return 1
	reads = [e for e in (args.read.split(",") if args.read else []) if e.strip()]
	# validate expressions up front (clear error before we attach a debugger)
	for e in reads:
		try:
			_parse_read_expr(e)
		except ValueError as ex:
			print(f"error: {ex}", file=sys.stderr)
			return 1

	lp = LiveProcess.attach(args.pid)
	modname = os.path.basename(args.exe or get_binary(con, args)).lower()
	modbase = lp.module_base(modname) or IMAGE_BASE_DEFAULT
	delta = modbase - IMAGE_BASE_DEFAULT

	def rebase_in(va):   # static VA -> live address
		return (va + delta) & 0xffffffff

	def rebase_out(live):  # live code address -> static VA (for symbol lookup)
		return (live - delta) & 0xffffffff

	def describe_eip(live_eip):
		sva = rebase_out(live_eip)
		name, tag = resolve_addr(con, sva)
		return f"0x{live_eip:08x} (static 0x{sva:08x}: {name} {tag})"

	state = {"phase": 1, "captured": [], "remaining": max(1, args.hits)}

	def _writer_line(regs, target):
		newv = lp.read(target, 4)
		print(f"  [write hit] writer EIP = {describe_eip(regs['eip'])}")
		print(f"    field @0x{target:08x} now = {_fmt_mem(newv, 'f32')} (f32) / "
			  f"{_fmt_mem(newv, 'hex')}")
		print(f"    regs: eax=0x{regs['eax']:08x} ecx=0x{regs['ecx']:08x} "
			  f"edx=0x{regs['edx']:08x} esi=0x{regs['esi']:08x} edi=0x{regs['edi']:08x} "
			  f"ebp=0x{regs['ebp']:08x}")

	def on_hit(dbg, ctx, armed_addr, armed_kind):
		regs = _ctx_regs(ctx)
		if args.mode == "field" and state["phase"] == 1:
			base = regs[args.reg]
			off = int(args.off, 16) if args.off.lower().startswith("0x") else int(args.off)
			target = (base + off) & 0xffffffff
			cur = lp.read(target, 4)
			print(f"  [exec hit] {describe_eip(regs['eip'])}")
			print(f"    {args.reg}=0x{base:08x}  +0x{off:x} -> field @0x{target:08x}"
				  f"  current={_fmt_mem(cur, 'f32')} (f32) / {_fmt_mem(cur, 'hex')}")
			print(f"    arming WRITE breakpoint on 0x{target:08x} - waiting for the writer ...")
			state["phase"] = 2
			state["target"] = target
			return (target, "write")
		if args.mode == "field":  # phase 2 - the write(s) fired
			target = state["target"]
			_writer_line(regs, target)
			state["captured"].append(regs)
			state["remaining"] -= 1
			if state["remaining"] > 0:
				return (target, "write")  # keep watching for the next writer
			return None
		# write single-shot / multi-hit mode
		if args.mode == "write":
			_writer_line(regs, primary)
			state["captured"].append(regs)
			state["remaining"] -= 1
			if state["remaining"] > 0:
				return (primary, "write")
			return None
		# exec mode
		print(f"  [exec hit] {describe_eip(regs['eip'])}")
		print(f"    eax=0x{regs['eax']:08x} ebx=0x{regs['ebx']:08x} "
			  f"ecx=0x{regs['ecx']:08x} edx=0x{regs['edx']:08x}")
		print(f"    esi=0x{regs['esi']:08x} edi=0x{regs['edi']:08x} "
			  f"ebp=0x{regs['ebp']:08x} esp=0x{regs['esp']:08x}")
		if reads:
			for line in _eval_reads(reads, regs, lp):
				print(f"    {line}")
		state["captured"].append(regs)
		state["remaining"] -= 1
		if state["remaining"] > 0:
			return (armed_addr, "exec")
		return None

	# Arm address: code VAs are rebased; write targets in `write` mode are taken
	# as absolute (heap) unless they look like a module VA.
	if args.mode == "write":
		is_code = IMAGE_BASE_DEFAULT <= primary < IMAGE_BASE_DEFAULT + 0x4000000
		arm_addr, arm_kind = (rebase_in(primary) if is_code else primary), "write"
	else:  # exec / field both start with an EXEC bp on a code VA
		arm_addr, arm_kind = rebase_in(primary), "exec"

	existing = _running_pids_by_name(modname)
	print(f"attaching debugger to pid {args.pid} ({modname}); "
		  f"module base 0x{modbase:08x} (delta {delta:+#x})")
	print(f"  arming {arm_kind.upper()} breakpoint @0x{arm_addr:08x}"
		  f"{'  (rebased from ' + args.addr + ')' if delta and arm_kind != 'write' else ''}; "
		  f"timeout {args.timeout:.0f}s ... (Ctrl-C-safe: detaches without killing)")
	dbg = Debugger(args.pid, verbose=args.verbose)
	try:
		hits = dbg.run(arm_addr, arm_kind, args.timeout, on_hit)
	except OSError as e:
		print(f"error: {e}", file=sys.stderr)
		lp.close()
		return 1
	finally:
		lp.close()
	if not hits:
		print(f"  no breakpoint hit within {args.timeout:.0f}s "
			  f"(is the client at the scene that executes this code? "
			  f"is the address right?)")
		return 1
	print(f"  detached cleanly ({hits} hit(s)); client still running.")
	return 0
