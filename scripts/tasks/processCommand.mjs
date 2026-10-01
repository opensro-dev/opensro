import { existsSync } from "node:fs";
import path from "node:path";
import { pythonExecutable } from "../build/shared/pythonRun.mjs";

let cachedPnpmCli;

export function resolveProcessCommand(command, args) {
  if (command === "node") {
    return { executable: process.execPath, args };
  }
  if (command === "python") {
    return { executable: pythonExecutable(), args };
  }
  if (command === "pnpm" && process.platform === "win32") {
    return { executable: process.execPath, args: [resolvePnpmCli(), ...args] };
  }
  return { executable: command, args };
}

function resolvePnpmCli() {
  if (cachedPnpmCli) return cachedPnpmCli;

  const explicit = process.env.npm_execpath;
  if (explicit && existsSync(explicit) && /pnpm(?:\.cjs|\.js)$/i.test(explicit)) {
    cachedPnpmCli = explicit;
    return cachedPnpmCli;
  }

  const searchRoots = [process.env.PNPM_HOME, ...(process.env.PATH ?? "").split(path.delimiter)]
    .filter(Boolean)
    .map((entry) => entry.replace(/^"|"$/g, ""));
  for (const root of searchRoots) {
    for (const relativePath of [
      "node_modules/pnpm/bin/pnpm.cjs",
      "node_modules/corepack/dist/pnpm.js"
    ]) {
      const candidate = path.join(root, relativePath);
      if (existsSync(candidate)) {
        cachedPnpmCli = candidate;
        return cachedPnpmCli;
      }
    }
  }

  throw new Error("Unable to resolve pnpm's JavaScript CLI from PATH; install the packageManager declared in package.json");
}
