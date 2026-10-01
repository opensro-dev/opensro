// Python subprocess runner for the resource pipeline (py -3 first, plain
// python second), with evidence-based failure attribution.
//
// Born from a real incident (2026-07-29): the Windows Font Cache Service held
// sro-default.ttf open, so `py -3` failed deep inside a WORKING fontTools with
// PermissionError at the final save. The old two-attempt loop then ran plain
// `python`, which failed for an unrelated reason (no fontTools in that
// interpreter), concatenated both errors with the last one visually dominant,
// and prefixed a fixed "Install Python fontTools" hint that the first attempt
// had already disproven. The file lock - the real cause - was buried.
//
// The rules this module enforces:
//   1. Fall back to the next interpreter ONLY for environment-class failures
//      (no interpreter launched, or a Python module failed to import). Once an
//      interpreter has executed the payload past its imports, its failure is
//      the authoritative one: throw it immediately, so a later attempt failing
//      for an unrelated environmental reason can never bury it.
//   2. Never emit a remediation hint the evidence contradicts. The hint is
//      selected from the CLASS of what actually failed: missing interpreter,
//      missing module, or the operation itself (which is not an environment
//      problem and is never reported as one).
//   3. Permission/lock failures name the exact path that could not be written
//      and the likely holder (the Windows Font Cache Service for font files).

import { spawn } from "node:child_process";

/**
 * One finished interpreter attempt, successful or not. Never rejects; the
 * caller classifies.
 * @typedef {object} PythonAttemptResult
 * @property {string} command executable name given to spawn
 * @property {string[]} args full argument vector
 * @property {Error | null} spawnError set when the process never launched
 * @property {number | null} exitCode null when killed by a signal or never launched
 * @property {string} stdout
 * @property {string} stderr
 */

/**
 * What an attempt's failure actually proves.
 *   - "no-interpreter": nothing ran Python at all (spawn failure, or the
 *     launcher/Store alias printed its "no Python here" text).
 *   - "missing-module": an interpreter ran but the payload died importing a
 *     module - a different interpreter may well have it, so falling back is
 *     legitimate.
 *   - "operation": the interpreter and its imports worked and the OPERATION
 *     failed. Deterministic w.r.t. the input and filesystem; retrying on a
 *     different interpreter is noise, so the caller must not fall back.
 * @typedef {object} PythonFailureClassification
 * @property {"no-interpreter" | "missing-module" | "operation"} kind
 * @property {string} summary one-line attribution for this attempt
 * @property {{ path: string | null, line: string } | null} permission set when
 *   the operation failure is a permission/lock error
 */

const PYTHON_TRACEBACK_PATTERN = /^Traceback \(most recent call last\)/m;
const PYTHON_STRING_FRAME_PATTERN = /^\s*File "<(?:string|stdin)>"/m;
const PYTHON_EXCEPTION_LINE_PATTERN = /^[A-Za-z_][\w.]*(?:Error|Exception|Interrupt|Warning)\b\s*(?::|$)/;
const MODULE_MISSING_PATTERN = /^(?:ModuleNotFoundError|ImportError)\b/;
const PERMISSION_LINE_PATTERN = /PermissionError|\[WinError 5\]|\[Errno 1[36]\]|EACCES|EPERM|EBUSY/;
// Ways Windows says "that command exists but there is no Python behind it":
// the Store alias stub, the py launcher's version miss, and cmd's not-found.
const NO_INTERPRETER_PATTERNS = [
  /Python was not found/i,
  /No suitable Python/i,
  /Requested Python version .* not (?:found|installed)/i,
  /Can't find a (?:usable|default) (?:init\.tcl|Python)/i,
  /Unable to create process using/i,
  /is not recognized as an internal or external command/i
];
const STDERR_TAIL_LIMIT = 6000;

/**
 * Holder attribution for a file a write could not touch. Shared wording with
 * resourceIo's publishFileFromTemp so Python-side and Node-side permission
 * failures diagnose identically.
 * @param {string} filePath
 * @returns {string}
 */
export function describeHeldFile(filePath) {
  const fontLike = /\.(ttf|ttc|otf)(\.tmp)?$/i.test(filePath);
  return (
    "another process may be holding it open (" +
    (fontLike ? "on Windows the Font Cache Service is a known culprit for .ttf files; " : "") +
    "antivirus scanners and dev servers also pin freshly written files)"
  );
}

/**
 * Classify what a failed attempt proves about the environment vs the
 * operation. Exported for direct inspection/testing.
 * @param {PythonAttemptResult} attempt
 * @returns {PythonFailureClassification}
 */
export function classifyPythonFailure(attempt) {
  if (attempt.spawnError !== null) {
    return {
      kind: "no-interpreter",
      summary: `interpreter did not run (spawn: ${attempt.spawnError.message})`,
      permission: null
    };
  }

  const stderr = attempt.stderr.trim();
  const stdout = attempt.stdout.trim();
  const diagnostic = [stderr, stdout].filter((stream) => stream.length > 0).join("\n");
  const lines = diagnostic.split(/\r?\n/).map((line) => line.trim()).filter((line) => line.length > 0);
  const lastLine = lines.length > 0 ? lines[lines.length - 1] : "";
  const firstLine = lines.length > 0 ? lines[0] : "";

  if (MODULE_MISSING_PATTERN.test(lastLine)) {
    return {
      kind: "missing-module",
      summary: `interpreter ran but a required module is missing (${lastLine})`,
      permission: null
    };
  }

  const pythonRanThePayload =
    PYTHON_TRACEBACK_PATTERN.test(diagnostic) ||
    PYTHON_STRING_FRAME_PATTERN.test(diagnostic) ||
    PYTHON_EXCEPTION_LINE_PATTERN.test(lastLine);
  if (pythonRanThePayload) {
    const permissionLine = [...lines].reverse().find((line) => PERMISSION_LINE_PATTERN.test(line)) ?? null;
    const pathMatch = permissionLine !== null ? permissionLine.match(/'([^']+)'\s*$/) ?? permissionLine.match(/'([^']+)'/) : null;
    // The quoted path is a Python repr: undo its backslash escaping.
    const heldPath = pathMatch !== null ? pathMatch[1].replace(/\\\\/g, "\\") : null;
    return {
      kind: "operation",
      summary: `the operation failed (${lastLine})`,
      permission: permissionLine !== null ? { path: heldPath, line: permissionLine } : null
    };
  }

  if (NO_INTERPRETER_PATTERNS.some((pattern) => pattern.test(diagnostic))) {
    return {
      kind: "no-interpreter",
      summary: `interpreter did not run (${firstLine})`,
      permission: null
    };
  }

  // A non-zero payload that emitted stdout got far enough to run its
  // operation.  Audit/check commands commonly reserve stderr for crashes and
  // print their expected failure report to stdout before exiting 1.  Treating
  // that shape as an unknown environment failure retries another interpreter
  // and, worse, drops the only diagnostic (the WIP proof gate is one concrete
  // example).  Non-empty stdout is therefore positive execution evidence,
  // not a reason to fall back.
  if (attempt.stdout.trim().length > 0) {
    const firstOutputLine = attempt.stdout
      .split(/\r?\n/)
      .map((line) => line.trim())
      .find((line) => line.length > 0) ?? "non-empty stdout";
    return {
      kind: "operation",
      summary: `the operation reported failure (${firstOutputLine})`,
      permission: null
    };
  }

  return {
    kind: "operation",
    summary:
      attempt.exitCode === null
        ? `the launched payload died from a signal (${lastLine || "no diagnostic output"})`
        : `the launched payload exited ${attempt.exitCode} (${lastLine || "no diagnostic output"})`,
    permission: null
  };
}

/**
 * The interpreter a plain `python` command means on this host: SRO_PYTHON,
 * else `python` on Windows and `python3` elsewhere (no bare `python` on macOS).
 * @returns {string}
 */
export function pythonExecutable() {
  return process.env.SRO_PYTHON || (process.platform === "win32" ? "python" : "python3");
}

/**
 * Interpreter candidates in order: SRO_PYTHON when set, then `py -3` and
 * `python` on Windows, or `python3` and `python` elsewhere (macOS and Linux
 * ship no `py` launcher).
 * @param {string[]} pythonArgs
 * @returns {{ label: string, command: string, args: string[] }[]}
 */
export function pythonAttempts(pythonArgs) {
  const attempts = [];
  if (process.env.SRO_PYTHON) {
    attempts.push({ label: "SRO_PYTHON", command: process.env.SRO_PYTHON, args: [...pythonArgs] });
  }
  if (process.platform === "win32") {
    attempts.push({ label: "py -3", command: "py", args: ["-3", ...pythonArgs] });
  } else {
    attempts.push({ label: "python3", command: "python3", args: [...pythonArgs] });
  }
  attempts.push({ label: "python", command: "python", args: [...pythonArgs] });
  return attempts;
}

/**
 * Run a Python payload, the first candidate of pythonAttempts() first and the rest as fallbacks -
 * but ONLY for environment-class failures. An operation failure (interpreter
 * and imports fine, the work itself died) throws immediately with that
 * attempt's evidence front and center.
 * @param {string[]} pythonArgs arguments for the interpreter (e.g. ["-c", code, ...] or [scriptPath, ...])
 * @param {{ task: string, context?: string[], cwd?: string }} options `task`
 *   names the operation for messages; `context` lines carry caller-specific
 *   stakes; `cwd` defaults to the current working directory
 * @returns {Promise<{ command: string, stdout: string, stderr: string }>} the
 *   winning attempt's label (see pythonAttempts) and captured output
 */
export async function runPython(pythonArgs, options) {
  const context = options.context ?? [];
  const cwd = options.cwd ?? process.cwd();
  const attempts = pythonAttempts(pythonArgs);

  /** @type {{ label: string, classification: PythonFailureClassification, stdout: string, stderr: string }[]} */
  const environmentFailures = [];
  for (const attempt of attempts) {
    const result = await runPythonAttempt(attempt.command, attempt.args, cwd);
    if (result.spawnError === null && result.exitCode === 0) {
      return { command: attempt.label, stdout: result.stdout, stderr: result.stderr };
    }

    const classification = classifyPythonFailure(result);
    if (classification.kind === "operation") {
      throw new Error(formatOperationFailure(options.task, context, attempt.label, result, classification));
    }
    environmentFailures.push({
      label: attempt.label,
      classification,
      stdout: result.stdout,
      stderr: result.stderr
    });
  }

  throw new Error(formatEnvironmentFailure(options.task, context, environmentFailures));
}

/**
 * Spawn one interpreter attempt and collect everything; never rejects.
 * @param {string} command
 * @param {string[]} args
 * @param {string} cwd
 * @returns {Promise<PythonAttemptResult>}
 */
function runPythonAttempt(command, args, cwd) {
  return new Promise((resolve) => {
    const child = spawn(command, args, { cwd, windowsHide: true });
    let stdout = "";
    let stderr = "";
    let settled = false;

    child.stdout.on("data", (chunk) => {
      stdout += chunk;
    });
    child.stderr.on("data", (chunk) => {
      stderr += chunk;
    });
    child.on("error", (error) => {
      if (settled) return;
      settled = true;
      resolve({ command, args, spawnError: error, exitCode: null, stdout, stderr });
    });
    child.on("close", (code) => {
      if (settled) return;
      settled = true;
      resolve({ command, args, spawnError: null, exitCode: code, stdout, stderr });
    });
  });
}

/**
 * The authoritative message for an operation failure: names what worked (the
 * environment), what failed (the operation), the held path when it is a
 * permission/lock death, and the full stderr last so the traceback stays
 * available without dominating the diagnosis.
 * @param {string} task
 * @param {string[]} context
 * @param {string} label
 * @param {PythonAttemptResult} result
 * @param {PythonFailureClassification} classification
 * @returns {string}
 */
function formatOperationFailure(task, context, label, result, classification) {
  const lines = [
    `${task} failed.`,
    ...context,
    `\`${label}\` ran Python and the operation itself failed - the interpreter and its imports are fine, ` +
      `so this is NOT a missing-Python or missing-module problem; do not reinstall anything over it.`
  ];
  if (classification.permission !== null) {
    lines.push(
      classification.permission.path !== null
        ? `Permission/lock failure: could not write '${classification.permission.path}' - ` +
            `${describeHeldFile(classification.permission.path)}.`
        : `Permission/lock failure (${classification.permission.line}) - another process may be holding the target open.`
    );
  }
  if (result.stdout.trim().length > 0) {
    lines.push(`--- stdout (${label}) ---`, trimOutputTail(result.stdout));
  }
  if (result.stderr.trim().length > 0) {
    lines.push(`--- stderr (${label}) ---`, trimOutputTail(result.stderr));
  }
  if (result.stdout.trim().length === 0 && result.stderr.trim().length === 0) {
    lines.push(`--- output (${label}) ---`, "(empty)");
  }
  return lines.join("\n");
}

/**
 * The message when no attempt got past the environment: one attribution line
 * per attempt in the order tried, then a hint selected from what the failures
 * collectively prove - never a hint any attempt's evidence contradicts.
 * @param {string} task
 * @param {string[]} context
 * @param {{ label: string, classification: PythonFailureClassification, stdout: string, stderr: string }[]} failures
 * @returns {string}
 */
function formatEnvironmentFailure(task, context, failures) {
  const lines = [
    `${task} failed: every Python attempt failed before the operation could run.`,
    ...context,
    ...failures.map((failure) => `  ${failure.label.padEnd(6)} -> ${failure.classification.summary}`)
  ];

  const kinds = new Set(failures.map((failure) => failure.classification.kind));
  if (kinds.has("missing-module")) {
    lines.push(
      "A Python 3 interpreter is present but a required module is not importable from it - " +
        "do NOT reinstall Python; install the build dependencies instead:",
      "  py -3 -m pip install -r rebuild/requirements.txt"
    );
  } else {
    lines.push("No usable Python interpreter was found: install Python 3 and make sure the `py` launcher is on PATH.");
  }

  for (const failure of failures) {
    if (failure.stdout.trim().length > 0) {
      lines.push(`--- stdout (${failure.label}) ---`, trimOutputTail(failure.stdout));
    }
    if (failure.stderr.trim().length > 0) {
      lines.push(`--- stderr (${failure.label}) ---`, trimOutputTail(failure.stderr));
    }
  }
  return lines.join("\n");
}

/**
 * Keep the tail of a long stream - the final exception lines are the
 * diagnosis; the head of a huge traceback is not worth the noise.
 * @param {string} output
 * @returns {string}
 */
function trimOutputTail(output) {
  const trimmed = output.trim();
  if (trimmed.length <= STDERR_TAIL_LIMIT) {
    return trimmed;
  }
  return `[... ${trimmed.length - STDERR_TAIL_LIMIT} chars trimmed ...]\n${trimmed.slice(-STDERR_TAIL_LIMIT)}`;
}
