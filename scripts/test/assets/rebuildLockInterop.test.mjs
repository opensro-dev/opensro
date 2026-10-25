import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { existsSync } from "node:fs";
import { readFile, rm } from "node:fs/promises";
import path from "node:path";
import { after, test } from "node:test";
import { fileURLToPath } from "node:url";
import { withRebuildLock } from "../../rebuildLock.mjs";

// Contract tests for the two generated-assets lock implementations: rebuildLock.mjs
// (Node) and rebuild_lock.py (Python, used by convert_images.py). Both manage the same
// on-disk lock directories under rebuild/.state/locks, so each side must read the
// other's owner.json as a LIVE lock. Two regressions are pinned here:
//
// 1. Timestamp precision: Node writes millisecond-precision ISO timestamps
//    (`new Date().toISOString()`), and Python's parse_iso_ms only accepted second
//    precision, so a Node heartbeat parsed to 0, its age computed as ~the full epoch,
//    and Python deleted a live Node-held lock as stale. Pinned by the AGE_MS check.
//
// 2. Liveness probe: Python's process_alive used os.kill(pid, 0), which on Windows is
//    delivered via TerminateProcess -- it KILLED the live Node lock holder (with exit
//    code 0). Pinned by holding the Node lock in a separate spawned child and asserting
//    it survives the Python contender; the holder must NOT be this test process, or a
//    regression terminates the test runner with a clean exit code instead of failing.
//
// Known cosmetic divergences, documented rather than asserted away:
// - token: Node appends a random hex suffix (`pid-ms-rand`), Python writes `pid-ms`.
// - name normalization: Python strips leading/trailing "-", Node does not.
//
// The locks root is hardcoded in both implementations, so these tests use the real
// root with unique per-pid lock names -- never the live "generated-assets" name --
// and remove every directory they created on the way out.

const testDir = path.dirname(fileURLToPath(import.meta.url));
const scriptsDir = path.resolve(testDir, "..", "..");
const rebuildRoot = path.resolve(scriptsDir, "..");
const locksRoot = path.join(rebuildRoot, ".state", "locks");

const LOCK_NAME_BASE = `lock-contract-${process.pid}`;
const LOCK_ENV_VARS = [
  // Env-based re-entry: a child inheriting these SKIPS acquisition, which would
  // make the contention in these tests fake. Spawned drivers get them stripped.
  "SRO_REBUILD_LOCK_NAME",
  "SRO_REBUILD_LOCK_TOKEN",
  "SRO_REBUILD_LOCK_DIR",
  // Tuning knobs, stripped so ambient values cannot skew the short test timeouts.
  "SRO_REBUILD_LOCK_TIMEOUT_MS",
  "SRO_REBUILD_LOCK_POLL_MS",
  "SRO_REBUILD_LOCK_STALE_MS"
];

const OWNER_FIELDS = [
  "name",
  "label",
  "token",
  "pid",
  "ppid",
  "user",
  "host",
  "cwd",
  "command",
  "lockDir",
  "startedAt",
  "heartbeatAt",
  "mode"
];

// The Node in-process acquisitions below would otherwise short-circuit through the
// same re-entry envs if this test process was itself started under a lock wrapper.
for (const key of LOCK_ENV_VARS) {
  delete process.env[key];
}

const createdLockDirs = new Set();
const spawnedChildren = new Set();

// Acquires the lock, announces HELD, and holds until stdin delivers a byte.
const NODE_HOLDER_DRIVER = [
  "import path from 'node:path';",
  "import { pathToFileURL } from 'node:url';",
  "const modulePath = path.join(process.env.SRO_INTEROP_SCRIPTS_DIR, 'rebuildLock.mjs');",
  "const { withRebuildLock } = await import(pathToFileURL(modulePath).href);",
  "await withRebuildLock({ name: process.env.SRO_INTEROP_LOCK_NAME, label: 'interop-node-holder', timeoutMs: 5000 }, async () => {",
  "  console.log('HELD');",
  "  await new Promise((resolve) => process.stdin.once('data', resolve));",
  "});",
  "console.log('RELEASED');"
].join("\n");

// Reports the heartbeat age it parses from the current owner.json, then attempts the
// lock with a short timeout: CONTENTION is the correct outcome against a live holder.
const PYTHON_CONTENDER_DRIVER = [
  "import json, os, sys, time",
  "sys.path.insert(0, os.environ['SRO_INTEROP_SCRIPTS_DIR'])",
  "import rebuild_lock as rl",
  "name = os.environ['SRO_INTEROP_LOCK_NAME']",
  "owner_path = os.path.join(str(rl.LOCKS_ROOT), name + '.lock', 'owner.json')",
  "with open(owner_path, encoding='utf-8') as handle:",
  "    owner = json.load(handle)",
  "age_ms = int(time.time() * 1000) - rl.parse_iso_ms(str(owner['heartbeatAt']))",
  "print('AGE_MS=%d' % age_ms, flush=True)",
  "try:",
  "    with rl.rebuild_lock(name, 'interop-contender'):",
  "        print('ACQUIRED', flush=True)",
  "except TimeoutError:",
  "    print('CONTENTION', flush=True)"
].join("\n");

const PYTHON_HOLDER_DRIVER = [
  "import os, sys",
  "sys.path.insert(0, os.environ['SRO_INTEROP_SCRIPTS_DIR'])",
  "import rebuild_lock as rl",
  "with rl.rebuild_lock(os.environ['SRO_INTEROP_LOCK_NAME'], 'interop-holder'):",
  "    print('HELD', flush=True)",
  "    sys.stdin.readline()",
  "print('RELEASED', flush=True)"
].join("\n");

after(async () => {
  for (const child of spawnedChildren) {
    if (child.exitCode === null && !child.killed) {
      child.kill();
    }
  }
  for (const lockDir of createdLockDirs) {
    await rm(lockDir, { recursive: true, force: true });
  }
});

test("a live Node-held lock reads as held, not stale, from Python", async () => {
  const name = trackLockName(`${LOCK_NAME_BASE}-node-holds`);
  const holder = spawnDriver(process.execPath, ["--input-type=module", "-e", NODE_HOLDER_DRIVER], {
    SRO_INTEROP_LOCK_NAME: name
  });

  try {
    await waitForStdout(holder, "HELD", 20000);

    const contender = spawnDriver("py", ["-c", PYTHON_CONTENDER_DRIVER], {
      SRO_INTEROP_LOCK_NAME: name,
      SRO_REBUILD_LOCK_TIMEOUT_MS: "1500",
      SRO_REBUILD_LOCK_POLL_MS: "250"
    });
    const contenderExit = await waitForExit(contender.child, 30000);

    // Regression pin #1: heartbeatAt in owner.json was just written by Node with
    // millisecond precision. Before the parse_iso_ms fix this age computed as the
    // full epoch (~1.7e12 ms) and Python stole the lock.
    const ageMatch = contender.output.stdout.match(/AGE_MS=(-?\d+)/);
    assert.ok(
      ageMatch,
      `python driver printed no AGE_MS; stdout: ${contender.output.stdout} stderr: ${contender.output.stderr}`
    );
    const ageMs = Number(ageMatch[1]);
    assert.ok(
      ageMs >= -5000 && ageMs <= 60000,
      `python computed heartbeat age ${ageMs}ms from a Node ms-precision timestamp; expected a small age`
    );

    assert.match(contender.output.stdout, /CONTENTION/, `stderr: ${contender.output.stderr}`);
    assert.doesNotMatch(contender.output.stdout, /ACQUIRED/, "python stole a live Node-held lock");
    assert.equal(contenderExit, 0, `stderr: ${contender.output.stderr}`);

    // Regression pin #2: the holder must have SURVIVED the python liveness probe.
    // With os.kill(pid, 0) on Windows the probe terminated it (exit code 0).
    assert.equal(holder.child.exitCode, null, "python's liveness probe killed the live Node lock holder");
    assert.ok(existsSync(lockDirFor(name)), "the Node-held lock directory should have survived the python contender");
  } finally {
    releaseHolder(holder);
    await waitForExit(holder.child, 15000);
  }

  assert.match(holder.output.stdout, /RELEASED/, `holder stderr: ${holder.output.stderr}`);
  assert.equal(existsSync(lockDirFor(name)), false, "node release should have removed the lock directory");
});

test("a live Python-held lock reads as held from Node", async () => {
  const name = trackLockName(`${LOCK_NAME_BASE}-python-holds`);
  const holder = spawnDriver("py", ["-c", PYTHON_HOLDER_DRIVER], { SRO_INTEROP_LOCK_NAME: name });

  try {
    await waitForStdout(holder, "HELD", 20000);

    let entered = false;
    await assert.rejects(
      withRebuildLock({ name, label: "interop node contender", timeoutMs: 1500, pollMs: 250 }, async () => {
        entered = true;
      }),
      /timed out waiting/
    );
    assert.equal(entered, false, "node stole a live Python-held lock");
    assert.equal(holder.child.exitCode, null, "node's liveness probe killed the live Python lock holder");
    assert.ok(existsSync(lockDirFor(name)), "the Python-held lock directory should have survived the node contender");
  } finally {
    releaseHolder(holder);
    await waitForExit(holder.child, 15000);
  }

  assert.equal(existsSync(lockDirFor(name)), false, "python release should have removed the lock directory");
});

test("both implementations write the same owner.json field-name set", async () => {
  const name = trackLockName(`${LOCK_NAME_BASE}-shape`);

  let nodeKeys;
  await withRebuildLock({ name, label: "interop shape probe", timeoutMs: 5000 }, async () => {
    nodeKeys = Object.keys(await readOwnerJson(name)).sort();
  });

  const holder = spawnDriver("py", ["-c", PYTHON_HOLDER_DRIVER], { SRO_INTEROP_LOCK_NAME: name });
  let pythonKeys;
  try {
    await waitForStdout(holder, "HELD", 20000);
    pythonKeys = Object.keys(await readOwnerJson(name)).sort();
  } finally {
    releaseHolder(holder);
    await waitForExit(holder.child, 15000);
  }

  assert.deepEqual(nodeKeys, [...OWNER_FIELDS].sort());
  assert.deepEqual(pythonKeys, nodeKeys);
});

function trackLockName(name) {
  createdLockDirs.add(lockDirFor(name));
  return name;
}

function lockDirFor(name) {
  return path.join(locksRoot, `${name}.lock`);
}

async function readOwnerJson(name) {
  return JSON.parse(await readFile(path.join(lockDirFor(name), "owner.json"), "utf8"));
}

function spawnDriver(command, args, extraEnv) {
  const env = { ...process.env };
  for (const key of LOCK_ENV_VARS) {
    delete env[key];
  }
  Object.assign(env, { SRO_INTEROP_SCRIPTS_DIR: scriptsDir }, extraEnv);

  const child = spawn(command, args, {
    cwd: rebuildRoot,
    env,
    stdio: ["pipe", "pipe", "pipe"],
    windowsHide: true
  });
  spawnedChildren.add(child);

  const output = { stdout: "", stderr: "" };
  child.stdout.on("data", (chunk) => {
    output.stdout += chunk;
  });
  child.stderr.on("data", (chunk) => {
    output.stderr += chunk;
  });
  return { child, output };
}

function releaseHolder(holder) {
  try {
    holder.child.stdin.write("\n");
    holder.child.stdin.end();
  } catch {
    // Child already gone; waitForExit's kill fallback covers it.
  }
}

async function waitForStdout(driver, marker, timeoutMs) {
  const deadline = Date.now() + timeoutMs;
  while (!driver.output.stdout.includes(marker)) {
    if (driver.child.exitCode !== null) {
      throw new Error(
        `driver exited (${driver.child.exitCode}) before printing ${marker}; stderr: ${driver.output.stderr}`
      );
    }
    if (Date.now() > deadline) {
      driver.child.kill();
      throw new Error(`timed out waiting for driver to print ${marker}; stderr: ${driver.output.stderr}`);
    }
    await sleep(50);
  }
}

function waitForExit(child, timeoutMs) {
  return new Promise((resolve) => {
    if (child.exitCode !== null) {
      resolve(child.exitCode);
      return;
    }
    const killTimer = setTimeout(() => child.kill(), timeoutMs);
    child.once("exit", (code) => {
      clearTimeout(killTimer);
      resolve(code);
    });
  });
}

function sleep(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}
