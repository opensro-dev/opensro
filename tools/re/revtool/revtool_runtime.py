"""
===========================================================================
revtool_runtime.py - the live-process reader and debugger behind runtime/break

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

from revtool_claims import reconcile_assertions
from revtool_d3d import IMAGE_BASE_DEFAULT
from revtool_pe import resolve_addr
from revtool_store import get_author, now_iso

class LiveProcess:
	"""ctypes wrapper around OpenProcess / ReadProcessMemory / module enumeration,
    with an optional launch+terminate lifecycle. Windows-only, read-only."""
	PROCESS_ACCESS = 0x0010 | 0x0400 | 0x0001 | 0x00100000  # VM_READ|QUERY_INFO|TERMINATE|SYNC
	TH32CS_SNAPMODULE = 0x8
	TH32CS_SNAPMODULE32 = 0x10

	def __init__(self, pid, owned=False, popen=None):
		import ctypes
		from ctypes import wintypes
		self.pid = pid
		self.owned = owned
		self._popen = popen
		self._ct = ctypes
		self._k = ctypes.WinDLL("kernel32", use_last_error=True)
		self._k.OpenProcess.restype = wintypes.HANDLE
		self._k.OpenProcess.argtypes = [wintypes.DWORD, wintypes.BOOL, wintypes.DWORD]
		self._k.ReadProcessMemory.restype = wintypes.BOOL
		self._k.ReadProcessMemory.argtypes = [wintypes.HANDLE, wintypes.LPCVOID,
											  wintypes.LPVOID, ctypes.c_size_t,
											  ctypes.POINTER(ctypes.c_size_t)]
		self._k.CreateToolhelp32Snapshot.restype = wintypes.HANDLE
		self._k.CreateToolhelp32Snapshot.argtypes = [wintypes.DWORD, wintypes.DWORD]
		self._k.CloseHandle.argtypes = [wintypes.HANDLE]
		self._k.TerminateProcess.restype = wintypes.BOOL
		self._k.TerminateProcess.argtypes = [wintypes.HANDLE, wintypes.UINT]
		self.h = self._k.OpenProcess(self.PROCESS_ACCESS, False, pid)
		if not self.h:
			raise OSError(f"OpenProcess(pid={pid}) failed (err {ctypes.get_last_error()}); "
						  f"is the process still alive / do you need admin?")

	@classmethod
	def attach(cls, pid):
		return cls(pid, owned=False)

	@classmethod
	def launch(cls, exe, extra_args=None, cwd=None):
		import subprocess
		if not os.path.exists(exe):
			raise OSError(f"exe not found: {exe}")
		si = subprocess.STARTUPINFO()
		si.dwFlags |= subprocess.STARTF_USESHOWWINDOW
		si.wShowWindow = 4  # SW_SHOWNOACTIVATE - render without stealing focus; the caller
		#                     then shoves the window off-screen (off-screen keeps rendering,
		#                     minimise can pause D3D). The game re-shows its own window anyway.
		cmd = [exe] + (list(extra_args) if extra_args else [])
		p = subprocess.Popen(cmd, cwd=cwd or os.path.dirname(exe) or None,
							 startupinfo=si, close_fds=True)
		return cls(p.pid, owned=True, popen=p)

	def module_base(self, name_lower):
		"""Runtime base of a loaded module (e.g. 'sro_client.exe'), or None if not yet
        mapped. Lets us rebase static VAs even if the loader ever relocates the image."""
		ct = self._ct
		from ctypes import wintypes

		class ME32(ct.Structure):
			_fields_ = [("dwSize", wintypes.DWORD), ("th32ModuleID", wintypes.DWORD),
						("th32ProcessID", wintypes.DWORD), ("GlblcntUsage", wintypes.DWORD),
						("ProccntUsage", wintypes.DWORD), ("modBaseAddr", ct.POINTER(ct.c_byte)),
						("modBaseSize", wintypes.DWORD), ("hModule", wintypes.HMODULE),
						("szModule", ct.c_char * 256), ("szExePath", ct.c_char * 260)]

		self._k.Module32First.argtypes = [wintypes.HANDLE, ct.POINTER(ME32)]
		self._k.Module32Next.argtypes = [wintypes.HANDLE, ct.POINTER(ME32)]
		invalid = ct.c_void_p(-1).value
		snap = self._k.CreateToolhelp32Snapshot(
			self.TH32CS_SNAPMODULE | self.TH32CS_SNAPMODULE32, self.pid)
		if not snap or snap == invalid:
			return None
		try:
			me = ME32()
			me.dwSize = ct.sizeof(ME32)
			if not self._k.Module32First(snap, ct.byref(me)):
				return None
			while True:
				if me.szModule.decode("ascii", "ignore").lower() == name_lower:
					return ct.cast(me.modBaseAddr, ct.c_void_p).value
				if not self._k.Module32Next(snap, ct.byref(me)):
					return None
		finally:
			self._k.CloseHandle(snap)

	def read(self, addr, n):
		ct = self._ct
		buf = ct.create_string_buffer(n)
		got = ct.c_size_t(0)
		ok = self._k.ReadProcessMemory(self.h, ct.c_void_p(addr), buf, n, ct.byref(got))
		if not ok or got.value < n:
			return None
		return buf.raw[:got.value]

	def alive(self):
		if self._popen is not None:
			return self._popen.poll() is None
		return True

	def move_offscreen(self):
		"""Shove every top-level window this process owns far off-screen. Off-screen keeps
        the app RENDERING (unlike minimise, which makes many D3D apps pause the render loop,
        so per-draw values never update); the window just never appears on a monitor.
        The game calls ShowWindow on its own window after creation, overriding the launch
        STARTUPINFO hint, so we re-place it here (idempotent, call each poll). Returns #moved."""
		import ctypes
		from ctypes import wintypes
		if not hasattr(self, "_u"):
			self._u = ctypes.WinDLL("user32", use_last_error=True)
			self._u.GetWindowThreadProcessId.argtypes = [wintypes.HWND,
														 ctypes.POINTER(wintypes.DWORD)]
			self._u.IsWindowVisible.argtypes = [wintypes.HWND]
			self._u.SetWindowPos.argtypes = [wintypes.HWND, wintypes.HWND, ctypes.c_int,
											 ctypes.c_int, ctypes.c_int, ctypes.c_int,
											 wintypes.UINT]
			self._u.EnumWindows.argtypes = [ctypes.c_void_p, wintypes.LPARAM]
		u = self._u
		moved = [0]
		SWP = 0x1 | 0x4 | 0x10  # NOSIZE | NOZORDER | NOACTIVATE
		WNDENUMPROC = ctypes.WINFUNCTYPE(wintypes.BOOL, wintypes.HWND, wintypes.LPARAM)

		def cb(hwnd, _lparam):
			pid = wintypes.DWORD(0)
			u.GetWindowThreadProcessId(hwnd, ctypes.byref(pid))
			if pid.value == self.pid and u.IsWindowVisible(hwnd):
				u.SetWindowPos(hwnd, 0, -32000, -32000, 0, 0, SWP)
				moved[0] += 1
			return True

		u.EnumWindows(WNDENUMPROC(cb), 0)
		return moved[0]

	def terminate(self):
		try:
			self._k.TerminateProcess(self.h, 0)
		except OSError:
			pass
		if self._popen is not None:
			try:
				self._popen.wait(timeout=5)
			except Exception:
				pass

	def close(self):
		if self.h:
			self._k.CloseHandle(self.h)
			self.h = None


# --------------------------------------------------------------------------
# Hardware-breakpoint debugger (Windows, 32-bit x86 target).
#
# Uses the Win32 debug API + CPU debug registers DR0-DR7 - NO code patching, no
# INT3 (0xCC) bytes written into the target. DebugSetProcessKillOnExit(FALSE) is
# set right after attach, so detaching (or revtool exiting) NEVER kills the
# client you attached to. This answers questions static pseudocode + read-only
# RPM can't: "what instruction WRITES this field?" (data breakpoint) and "what
# are the registers when EIP reaches here?" (execute breakpoint).
#
# x86 CONTEXT layout is identical to WOW64_CONTEXT, so the same struct works
# whether revtool's Python is 32-bit (GetThreadContext) or 64-bit debugging a
# 32-bit target (Wow64GetThreadContext).
# --------------------------------------------------------------------------

CONTEXT_i386 = 0x00010000
_CONTEXT_CONTROL = CONTEXT_i386 | 0x1
_CONTEXT_INTEGER = CONTEXT_i386 | 0x2
_CONTEXT_SEGMENTS = CONTEXT_i386 | 0x4
_CONTEXT_FLOATING_POINT = CONTEXT_i386 | 0x8
_CONTEXT_DEBUG_REGISTERS = CONTEXT_i386 | 0x10
_CONTEXT_EXTENDED_REGISTERS = CONTEXT_i386 | 0x20
CONTEXT_ALL_X86 = (_CONTEXT_CONTROL | _CONTEXT_INTEGER | _CONTEXT_SEGMENTS |
				   _CONTEXT_FLOATING_POINT | _CONTEXT_DEBUG_REGISTERS |
				   _CONTEXT_EXTENDED_REGISTERS)


def _dbg_structs():
	"""Build (and cache) the ctypes structs for the debug API. Done lazily so
    importing revtool on a non-Windows box never touches Windows-only types."""
	import ctypes
	from ctypes import wintypes
	cache = getattr(_dbg_structs, "_cache", None)
	if cache is not None:
		return cache
	DWORD, LONG, WORD = wintypes.DWORD, wintypes.LONG, wintypes.WORD
	HANDLE, PVOID = wintypes.HANDLE, ctypes.c_void_p

	class FLOATING_SAVE_AREA(ctypes.Structure):
		_fields_ = [("ControlWord", DWORD), ("StatusWord", DWORD), ("TagWord", DWORD),
					("ErrorOffset", DWORD), ("ErrorSelector", DWORD), ("DataOffset", DWORD),
					("DataSelector", DWORD), ("RegisterArea", ctypes.c_byte * 80),
					("Cr0NpxState", DWORD)]

	class CONTEXT86(ctypes.Structure):
		_fields_ = [("ContextFlags", DWORD),
					("Dr0", DWORD), ("Dr1", DWORD), ("Dr2", DWORD), ("Dr3", DWORD),
					("Dr6", DWORD), ("Dr7", DWORD),
					("FloatSave", FLOATING_SAVE_AREA),
					("SegGs", DWORD), ("SegFs", DWORD), ("SegEs", DWORD), ("SegDs", DWORD),
					("Edi", DWORD), ("Esi", DWORD), ("Ebx", DWORD), ("Edx", DWORD),
					("Ecx", DWORD), ("Eax", DWORD),
					("Ebp", DWORD), ("Eip", DWORD), ("SegCs", DWORD), ("EFlags", DWORD),
					("Esp", DWORD), ("SegSs", DWORD),
					("ExtendedRegisters", ctypes.c_byte * 512)]

	class EXCEPTION_RECORD(ctypes.Structure):
		_fields_ = [("ExceptionCode", DWORD), ("ExceptionFlags", DWORD),
					("ExceptionRecord", PVOID), ("ExceptionAddress", PVOID),
					("NumberParameters", DWORD), ("ExceptionInformation", PVOID * 15)]

	class EXCEPTION_DEBUG_INFO(ctypes.Structure):
		_fields_ = [("ExceptionRecord", EXCEPTION_RECORD), ("dwFirstChance", DWORD)]

	class CREATE_THREAD_DEBUG_INFO(ctypes.Structure):
		_fields_ = [("hThread", HANDLE), ("lpThreadLocalBase", PVOID), ("lpStartAddress", PVOID)]

	class CREATE_PROCESS_DEBUG_INFO(ctypes.Structure):
		_fields_ = [("hFile", HANDLE), ("hProcess", HANDLE), ("hThread", HANDLE),
					("lpBaseOfImage", PVOID), ("dwDebugInfoFileOffset", DWORD),
					("nDebugInfoSize", DWORD), ("lpThreadLocalBase", PVOID),
					("lpStartAddress", PVOID), ("lpImageName", PVOID), ("fUnicode", WORD)]

	class LOAD_DLL_DEBUG_INFO(ctypes.Structure):
		_fields_ = [("hFile", HANDLE), ("lpBaseOfDll", PVOID), ("dwDebugInfoFileOffset", DWORD),
					("nDebugInfoSize", DWORD), ("lpImageName", PVOID), ("fUnicode", WORD)]

	class DEBUG_EVENT_U(ctypes.Union):
		_fields_ = [("Exception", EXCEPTION_DEBUG_INFO),
					("CreateThread", CREATE_THREAD_DEBUG_INFO),
					("CreateProcessInfo", CREATE_PROCESS_DEBUG_INFO),
					("LoadDll", LOAD_DLL_DEBUG_INFO),
					("raw", ctypes.c_byte * 184)]

	class DEBUG_EVENT(ctypes.Structure):
		_fields_ = [("dwDebugEventCode", DWORD), ("dwProcessId", DWORD),
					("dwThreadId", DWORD), ("u", DEBUG_EVENT_U)]

	class THREADENTRY32(ctypes.Structure):
		_fields_ = [("dwSize", DWORD), ("cntUsage", DWORD), ("th32ThreadID", DWORD),
					("th32OwnerProcessID", DWORD), ("tpBasePri", LONG),
					("tpDeltaPri", LONG), ("dwFlags", DWORD)]

	ns = type("DbgTypes", (), dict(
		CONTEXT86=CONTEXT86, DEBUG_EVENT=DEBUG_EVENT, THREADENTRY32=THREADENTRY32))
	_dbg_structs._cache = ns
	return ns


class Debugger:
	"""Win32 hardware-breakpoint debugger for the 32-bit client. Attaches to a
    RUNNING pid, arms a CPU debug register on every thread, runs the debug loop
    until the breakpoint fires (or timeout), captures the trapped thread's
    register context, then disarms every thread and detaches WITHOUT killing the
    target (DebugSetProcessKillOnExit FALSE)."""

	DBG_CONTINUE = 0x00010002
	DBG_EXCEPTION_NOT_HANDLED = 0x80010001
	# Native codes AND their WOW64 (32-bit-on-64-bit) equivalents. A 64-bit
	# debugger attached to a 32-bit target receives STATUS_WX86_* for the debug
	# traps, not the native STATUS_* - miss this and every HW-BP hit looks like a
	# foreign exception, gets passed through, and crashes the client.
	EXCEPTION_SINGLE_STEP = 0x80000004
	EXCEPTION_BREAKPOINT = 0x80000003
	STATUS_WX86_SINGLE_STEP = 0x4000001E
	STATUS_WX86_BREAKPOINT = 0x4000001F
	SINGLE_STEP_CODES = (0x80000004, 0x4000001E)
	BREAKPOINT_CODES = (0x80000003, 0x4000001F)
	EX_EXCEPTION = 1
	EX_CREATE_THREAD = 2
	EX_CREATE_PROCESS = 3
	EX_EXIT_PROCESS = 5
	EX_LOAD_DLL = 6
	TH32CS_SNAPTHREAD = 0x4
	THREAD_ACCESS = 0x1FFFFF  # THREAD_ALL_ACCESS

	def __init__(self, pid, verbose=False):
		import ctypes
		from ctypes import wintypes
		self.pid = pid
		self.verbose = verbose
		self.ct = ctypes
		self.t = _dbg_structs()
		self.is64 = sys.maxsize > 2 ** 32
		k = ctypes.WinDLL("kernel32", use_last_error=True)
		self.k = k
		k.DebugActiveProcess.argtypes = [wintypes.DWORD]
		k.DebugActiveProcess.restype = wintypes.BOOL
		k.DebugActiveProcessStop.argtypes = [wintypes.DWORD]
		k.DebugActiveProcessStop.restype = wintypes.BOOL
		k.DebugSetProcessKillOnExit.argtypes = [wintypes.BOOL]
		k.DebugSetProcessKillOnExit.restype = wintypes.BOOL
		k.WaitForDebugEvent.argtypes = [ctypes.POINTER(self.t.DEBUG_EVENT), wintypes.DWORD]
		k.WaitForDebugEvent.restype = wintypes.BOOL
		k.ContinueDebugEvent.argtypes = [wintypes.DWORD, wintypes.DWORD, wintypes.DWORD]
		k.ContinueDebugEvent.restype = wintypes.BOOL
		k.OpenThread.argtypes = [wintypes.DWORD, wintypes.BOOL, wintypes.DWORD]
		k.OpenThread.restype = wintypes.HANDLE
		k.SuspendThread.argtypes = [wintypes.HANDLE]
		k.SuspendThread.restype = wintypes.DWORD
		k.ResumeThread.argtypes = [wintypes.HANDLE]
		k.ResumeThread.restype = wintypes.DWORD
		k.CloseHandle.argtypes = [wintypes.HANDLE]
		k.CreateToolhelp32Snapshot.argtypes = [wintypes.DWORD, wintypes.DWORD]
		k.CreateToolhelp32Snapshot.restype = wintypes.HANDLE
		k.Thread32First.argtypes = [wintypes.HANDLE, ctypes.POINTER(self.t.THREADENTRY32)]
		k.Thread32Next.argtypes = [wintypes.HANDLE, ctypes.POINTER(self.t.THREADENTRY32)]
		k.ReadProcessMemory.argtypes = [wintypes.HANDLE, wintypes.LPCVOID, wintypes.LPVOID,
										ctypes.c_size_t, ctypes.POINTER(ctypes.c_size_t)]
		k.ReadProcessMemory.restype = wintypes.BOOL
		getname = "Wow64GetThreadContext" if self.is64 else "GetThreadContext"
		setname = "Wow64SetThreadContext" if self.is64 else "SetThreadContext"
		self._get = getattr(k, getname)
		self._set = getattr(k, setname)
		self._get.argtypes = [wintypes.HANDLE, ctypes.POINTER(self.t.CONTEXT86)]
		self._get.restype = wintypes.BOOL
		self._set.argtypes = [wintypes.HANDLE, ctypes.POINTER(self.t.CONTEXT86)]
		self._set.restype = wintypes.BOOL
		self.modbase = None
		self._attached = False

	def _thread_ids(self):
		ct = self.ct
		invalid = ct.c_void_p(-1).value
		snap = self.k.CreateToolhelp32Snapshot(self.TH32CS_SNAPTHREAD, 0)
		if not snap or snap == invalid:
			return []
		ids = []
		try:
			te = self.t.THREADENTRY32()
			te.dwSize = ct.sizeof(self.t.THREADENTRY32)
			ok = self.k.Thread32First(snap, ct.byref(te))
			while ok:
				if te.th32OwnerProcessID == self.pid:
					ids.append(te.th32ThreadID)
				ok = self.k.Thread32Next(snap, ct.byref(te))
		finally:
			self.k.CloseHandle(snap)
		return ids

	@staticmethod
	def _dr7_for(kind):
		"""DR7 bits for DR0 + the RW/LEN encoding. exec: RW=00 LEN=00. write:
        RW=01 LEN=11 (4 bytes). rw: RW=11 LEN=11."""
		rw_len = {"exec": (0b00, 0b00), "write": (0b01, 0b11), "rw": (0b11, 0b11)}[kind]
		rw, ln = rw_len
		return (1 << 0) | (rw << 16) | (ln << 18)  # L0 + RW0 + LEN0

	def _set_dr_handle(self, hthread, addr, kind):
		ct = self.ct
		ctx = self.t.CONTEXT86()
		ctx.ContextFlags = _CONTEXT_DEBUG_REGISTERS
		if not self._get(hthread, ct.byref(ctx)):
			return False
		if addr is None:
			ctx.Dr0 = 0
			ctx.Dr7 &= ~((1 << 0) | (0b1111 << 16))  # clear L0 + RW0/LEN0
		else:
			ctx.Dr0 = addr & 0xffffffff
			ctx.Dr6 = 0
			ctx.Dr7 = (ctx.Dr7 & ~(0b1111 << 16)) | self._dr7_for(kind)
		ctx.ContextFlags = _CONTEXT_DEBUG_REGISTERS
		return bool(self._set(hthread, ct.byref(ctx)))

	def _set_all(self, addr, kind):
		"""Arm (addr!=None) or disarm (addr=None) DR0 on every thread of the
        target. Each thread is explicitly SuspendThread'd around the context
        write so it is reliable BOTH inside a debug event (threads already frozen
        - suspend just nests) AND in the detach path after the loop (threads
        running - we must freeze them or the DR write silently no-ops, leaving a
        stale breakpoint that crashes the client once it fires). Best-effort."""
		n = 0
		for tid in self._thread_ids():
			ht = self.k.OpenThread(self.THREAD_ACCESS, False, tid)
			if not ht:
				continue
			try:
				suspended = self.k.SuspendThread(ht) != 0xffffffff
				if self._set_dr_handle(ht, addr, kind):
					n += 1
				if suspended:
					self.k.ResumeThread(ht)
			finally:
				self.k.CloseHandle(ht)
		return n

	def _get_ctx(self, tid):
		ct = self.ct
		ht = self.k.OpenThread(self.THREAD_ACCESS, False, tid)
		if not ht:
			return None, None
		ctx = self.t.CONTEXT86()
		ctx.ContextFlags = CONTEXT_ALL_X86
		ok = self._get(ht, ct.byref(ctx))
		return (ctx if ok else None), ht

	def run(self, addr, kind, timeout_s, on_hit, hide_window=None):
		"""Attach, arm DR0=addr (kind), pump the debug loop until on_hit signals
        stop or timeout. on_hit(self, ctx, addr, kind) -> None to stop, or
        (new_addr, new_kind) to re-arm (used to chain exec->write). Returns the
        number of breakpoint hits delivered to on_hit."""
		import time
		ct = self.ct
		if not self.k.DebugActiveProcess(self.pid):
			raise OSError(f"DebugActiveProcess(pid={self.pid}) failed "
						  f"(err {ct.get_last_error()}); another debugger attached, "
						  f"or insufficient rights?")
		self._attached = True
		koe = self.k.DebugSetProcessKillOnExit(False)  # detaching must NOT kill the client
		if self.verbose:
			print(f"    [dbg] attached; DebugSetProcessKillOnExit(FALSE)={bool(koe)} "
				  f"err={ct.get_last_error()}", file=sys.stderr)
		armed_addr, armed_kind = addr, kind
		evt = self.t.DEBUG_EVENT()
		end = time.time() + timeout_s
		hits = 0
		stop = False
		try:
			while not stop and time.time() < end:
				if not self.k.WaitForDebugEvent(ct.byref(evt), 1000):
					continue  # 1s slice timeout; outer while re-checks deadline
				code = evt.dwDebugEventCode
				tid = evt.dwThreadId
				status = self.DBG_CONTINUE
				if code == self.EX_CREATE_PROCESS:
					self.modbase = evt.u.CreateProcessInfo.lpBaseOfImage or 0
					if evt.u.CreateProcessInfo.hFile:
						self.k.CloseHandle(evt.u.CreateProcessInfo.hFile)
					armed = self._set_all(armed_addr, armed_kind)
					if self.verbose:
						print(f"    [dbg] CREATE_PROCESS base=0x{self.modbase or 0:08x}; "
							  f"armed DR0=0x{armed_addr:08x} ({armed_kind}) on {armed} thread(s)",
							  file=sys.stderr)
					if hide_window:
						try:
							hide_window()
						except Exception:
							pass
				elif code == self.EX_CREATE_THREAD:
					ok = self._set_dr_handle(evt.u.CreateThread.hThread, armed_addr, armed_kind)
					if self.verbose:
						print(f"    [dbg] CREATE_THREAD tid={tid} arm={bool(ok)}", file=sys.stderr)
				elif code == self.EX_LOAD_DLL:
					if evt.u.LoadDll.hFile:
						self.k.CloseHandle(evt.u.LoadDll.hFile)
				elif code == self.EX_EXIT_PROCESS:
					if self.verbose:
						print("    [dbg] EXIT_PROCESS (debuggee gone)", file=sys.stderr)
					stop = True
				elif code == self.EX_EXCEPTION:
					exc = evt.u.Exception.ExceptionRecord.ExceptionCode & 0xffffffff
					if self.verbose and exc not in self.BREAKPOINT_CODES:
						print(f"    [dbg] EXCEPTION 0x{exc:08x} tid={tid} "
							  f"firstchance={evt.u.Exception.dwFirstChance}", file=sys.stderr)
					if exc in self.BREAKPOINT_CODES:
						status = self.DBG_CONTINUE  # initial attach breakpoint - swallow
					elif exc in self.SINGLE_STEP_CODES:
						# A single-step exception in a process that never sets the
						# trap flag itself is ALWAYS one of our HW breakpoints. We
						# must consume it (DBG_CONTINUE) - handing it back to the app
						# would crash it. Identify the hit via DR6 (data BP) or, as a
						# fallback, EIP==armed exec address.
						status = self.DBG_CONTINUE
						ctx, ht = self._get_ctx(tid)
						eip = ctx.Eip & 0xffffffff if ctx else 0
						is_ours = ctx is not None and (
							(ctx.Dr6 & 0xF) or (armed_kind == "exec" and eip == (armed_addr & 0xffffffff)))
						if self.verbose:
							print(f"    [dbg] SINGLE_STEP tid={tid} "
								  f"Dr6=0x{(ctx.Dr6 if ctx else 0):08x} eip=0x{eip:08x} "
								  f"ours={is_ours}", file=sys.stderr)
						if is_ours:
							hits += 1
							prev_kind = armed_kind
							result = on_hit(self, ctx, armed_addr, armed_kind)
							if result is None:
								self._set_all(None, None)
								stop = True
							else:
								armed_addr, armed_kind = result
								self._set_all(None, None)
								self._set_all(armed_addr, armed_kind)
								# Re-firing an EXEC bp at the SAME EIP would trap on
								# the very same instruction again (it triggers BEFORE
								# execution). Set the Resume Flag so the CPU runs one
								# instruction before the instruction-bp re-checks.
								if prev_kind == "exec" and ht is not None:
									rf = self.t.CONTEXT86()
									rf.ContextFlags = _CONTEXT_CONTROL
									if self._get(ht, ct.byref(rf)):
										rf.EFlags |= 0x10000
										rf.ContextFlags = _CONTEXT_CONTROL
										self._set(ht, ct.byref(rf))
						if ht:
							self.k.CloseHandle(ht)
					else:
						status = self.DBG_EXCEPTION_NOT_HANDLED
				self.k.ContinueDebugEvent(evt.dwProcessId, tid, status)
				evt = self.t.DEBUG_EVENT()
		finally:
			try:
				self._set_all(None, None)
			except Exception:
				pass
			if self._attached:
				self.k.DebugActiveProcessStop(self.pid)
				self._attached = False
		return hits

	def read(self, addr, n):
		ct = self.ct
		# use a fresh OpenProcess-style read via the debuggee handle isn't kept;
		# callers pass a LiveProcess for reads. This helper exists for symmetry.
		raise NotImplementedError


def _ctx_regs(ctx):
	"""Pull the general-purpose + control registers out of a CONTEXT86 into a
    plain dict (all masked to 32-bit)."""
	return {
		"eax": ctx.Eax & 0xffffffff, "ebx": ctx.Ebx & 0xffffffff,
		"ecx": ctx.Ecx & 0xffffffff, "edx": ctx.Edx & 0xffffffff,
		"esi": ctx.Esi & 0xffffffff, "edi": ctx.Edi & 0xffffffff,
		"ebp": ctx.Ebp & 0xffffffff, "esp": ctx.Esp & 0xffffffff,
		"eip": ctx.Eip & 0xffffffff, "eflags": ctx.EFlags & 0xffffffff,
	}


def _print_runtime(con, base_va, words, modbase, elapsed, observe_subj, author_args, conn_commit):
	delta = (modbase - IMAGE_BASE_DEFAULT) if modbase else 0
	rebase = f"module base 0x{modbase:08x} (delta {delta:+#x})" if modbase \
		else "module base not found - assuming no relocation (delta 0)"
	import struct as _struct
	print(f"runtime 0x{base_va:08x}  read after {elapsed:.1f}s  [{rebase}]")
	first_obs = None
	for i, w in enumerate(words):
		va = base_va + i * 4
		line = f"  +0x{i*4:<3x} 0x{va:08x} = 0x{w:08x}"
		if IMAGE_BASE_DEFAULT <= w < IMAGE_BASE_DEFAULT + 0x4000000:
			name, tag = resolve_addr(con, w)
			line += f"  ->  {name}  {tag}"
			obs = f"0x{w:08x}"
		else:
			fval = _struct.unpack("<f", _struct.pack("<I", w))[0]
			si = w - 0x100000000 if w >= 0x80000000 else w
			sane = (fval == 0.0) or (1e-4 < abs(fval) < 1e9)
			fstr = f"{fval:.6g}" if sane else "denorm/NaN"
			line += f"   int={si}  float={fstr}"
			obs = fstr if sane else str(si)
		if i == 0:
			first_obs = obs
		print(line)
	if observe_subj:
		author = get_author(author_args)
		val = first_obs if first_obs is not None else f"0x{words[0]:08x}"
		con.execute("INSERT INTO observations(subject,value,source,author,ts) VALUES(?,?,?,?,?)",
					(observe_subj, val, "revtool runtime", author, now_iso()))
		results = reconcile_assertions(con, observe_subj, val, "revtool runtime", author)
		con.commit()
		print(f"  observed {observe_subj} = {val}")
		for r in results:
			print(f"    assert #{r['id']}: {r['verdict']}  ({r['detail']})")
	return 0


def _watch_float(w):
	import struct as _struct
	f = _struct.unpack("<f", _struct.pack("<I", w))[0]
	return f if (f == 0.0 or 1e-4 < abs(f) < 1e9) else None


def _print_watch(con, base_va, count, samples, modbase, args):
	"""Summarise what each offset DID across the whole sampling window - so per-frame /
    per-draw values that are 0 most of the time (and nonzero only mid-draw) are still seen,
    without having to snapshot the exact instant."""
	delta = (modbase - IMAGE_BASE_DEFAULT) if modbase else 0
	rebase = f"module base 0x{modbase:08x} (delta {delta:+#x})" if modbase \
		else "module base not found (delta 0)"
	n = len(samples)
	print(f"watch 0x{base_va:08x}  {n} samples / {args.timeout:.0f}s @ {args.interval:.0f}ms  [{rebase}]")
	if n == 0:
		print("  (no successful reads - process gone or address unmapped)")
		return 1

	def fmt(w):
		if IMAGE_BASE_DEFAULT <= w < IMAGE_BASE_DEFAULT + 0x4000000:
			name, _tag = resolve_addr(con, w)
			return name
		fv = _watch_float(w)
		return f"{fv:.6g}" if fv is not None else f"0x{w:08x}"

	first_obs = None
	for i in range(count):
		vals = [s[1][i] for s in samples]
		nz = sum(1 for v in vals if v != 0)
		distinct = []
		for v in vals:
			if v not in distinct:
				distinct.append(v)
		first_nz = next((s[0] for s in samples if s[1][i] != 0), None)
		last = vals[-1]
		shown = ", ".join(fmt(v) for v in distinct[:6])
		more = "" if len(distinct) <= 6 else f" +{len(distinct) - 6} more"
		fnz = f"first@{first_nz:.1f}s" if first_nz is not None else "NEVER nonzero"
		print(f"  +0x{i * 4:<3x} nonzero {nz}/{n}  distinct {len(distinct)} [{shown}{more}]  "
			  f"{fnz}  last={fmt(last)}")
		if i == 0:
			first_obs = fmt(last)
	if args.observe and first_obs is not None:
		author = get_author(args)
		con.execute("INSERT INTO observations(subject,value,source,author,ts) VALUES(?,?,?,?,?)",
					(args.observe, first_obs, "revtool runtime --watch", author, now_iso()))
		results = reconcile_assertions(con, args.observe, first_obs, "revtool runtime --watch", author)
		con.commit()
		print(f"  observed {args.observe} = {first_obs} (last sample)")
		for r in results:
			print(f"    assert #{r['id']}: {r['verdict']}  ({r['detail']})")
	return 0


def _running_pids_by_name(name):
	"""Return the PIDs of every running process whose image name == `name`
    (case-insensitive, e.g. "sro_client.exe"). Windows-only; uses the Toolhelp
    process snapshot so it never spawns a child or kills anything. Empty list on
    any failure (best-effort guard, never fatal)."""
	if sys.platform != "win32":
		return []
	import ctypes
	from ctypes import wintypes

	TH32CS_SNAPPROCESS = 0x00000002
	INVALID_HANDLE_VALUE = ctypes.c_void_p(-1).value

	class PROCESSENTRY32(ctypes.Structure):
		_fields_ = [
			("dwSize", wintypes.DWORD),
			("cntUsage", wintypes.DWORD),
			("th32ProcessID", wintypes.DWORD),
			("th32DefaultHeapID", ctypes.POINTER(ctypes.c_ulong)),
			("th32ModuleID", wintypes.DWORD),
			("cntThreads", wintypes.DWORD),
			("th32ParentProcessID", wintypes.DWORD),
			("pcPriClassBase", ctypes.c_long),
			("dwFlags", wintypes.DWORD),
			("szExeFile", ctypes.c_char * 260),
		]

	k32 = ctypes.windll.kernel32
	snap = k32.CreateToolhelp32Snapshot(TH32CS_SNAPPROCESS, 0)
	if snap == INVALID_HANDLE_VALUE or snap == -1:
		return []
	pids = []
	try:
		entry = PROCESSENTRY32()
		entry.dwSize = ctypes.sizeof(PROCESSENTRY32)
		ok = k32.Process32First(snap, ctypes.byref(entry))
		target = name.lower()
		while ok:
			exe = entry.szExeFile.decode("latin-1", "ignore").lower()
			if exe == target:
				pids.append(int(entry.th32ProcessID))
			ok = k32.Process32Next(snap, ctypes.byref(entry))
	finally:
		k32.CloseHandle(snap)
	return pids
