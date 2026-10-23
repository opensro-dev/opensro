import { parseArgs } from "node:util";

import { getTask, listTasks } from "./tasks/registry.mjs";
import { runTask } from "./tasks/runner.mjs";

const KIND_ORDER = ["dev", "build", "test", "check", "assets"];

async function main(args) {
  if (args.length === 0 || args[0] === "help" || args[0] === "--help" || args[0] === "-h") {
    printHelp();
    return;
  }

  if (args[0] === "list") {
    printList(args.slice(1));
    return;
  }

  if (args[0] === "explain") {
    printExplanation(args.slice(1));
    return;
  }

  const invocationArgs = args[0] === "run" ? args.slice(1) : args;
  const match = matchTask(invocationArgs);
  if (!match) {
    printUnknown(invocationArgs);
    process.exitCode = 2;
    return;
  }

  await runTask(match.task.name, match.forwardedArgs);
}

function matchTask(args) {
  const optionIndex = args.findIndex((arg) => arg === "--" || arg.startsWith("-"));
  const selectorEnd = optionIndex === -1 ? args.length : optionIndex;
  for (let end = selectorEnd; end > 0; end -= 1) {
    const requestedName = args.slice(0, end).join(":");
    const task = getTask(requestedName);
    if (task) {
      return { task, requestedName, forwardedArgs: args.slice(end) };
    }
  }
  return undefined;
}

function printList(args) {
  const { values } = parseArgs({
    args,
    options: {
      all: { type: "boolean", default: false },
      json: { type: "boolean", default: false },
      kind: { type: "string" }
    },
    allowPositionals: false,
    strict: true
  });
  const tasks = listTasks({ kind: values.kind, includeInternal: values.all });
  if (values.json) {
    console.log(JSON.stringify(tasks.map(publicTaskMetadata), null, 2));
    return;
  }
  if (tasks.length === 0) {
    console.log(`No tasks${values.kind ? ` with kind "${values.kind}"` : ""}.`);
    return;
  }

  const nameWidth = Math.max(...tasks.map((task) => task.name.length));
  const kindWidth = Math.max(...tasks.map((task) => task.kind.length));
  for (const kind of KIND_ORDER) {
    const group = tasks.filter((task) => task.kind === kind);
    if (group.length === 0) continue;
    console.log(`${kind}:`);
    for (const task of group) {
      console.log(`  ${task.name.padEnd(nameWidth)}  ${task.kind.padEnd(kindWidth)}  ${task.description}`);
    }
  }
}

function printExplanation(args) {
  const { values, positionals } = parseArgs({
    args,
    options: { json: { type: "boolean", default: false } },
    allowPositionals: true,
    strict: true
  });
  const match = matchTask(positionals);
  if (!match || match.forwardedArgs.length > 0) {
    throw new Error("Usage: pnpm task explain <task> [--json]");
  }
  const metadata = publicTaskMetadata(match.task);
  if (values.json) {
    console.log(JSON.stringify(metadata, null, 2));
    return;
  }
  console.log(`${metadata.name} - ${metadata.description}`);
  console.log(`kind: ${metadata.kind}`);
  console.log(`ci: ${metadata.ci ? "yes" : "no"}`);
  console.log(`requires: ${metadata.requires.length > 0 ? metadata.requires.join(", ") : "none"}`);
  console.log(`timeout: ${metadata.timeoutClass}`);
  console.log(`execution: ${formatRun(match.task.run)}`);
}

function publicTaskMetadata(task) {
  return {
    name: task.name,
    description: task.description,
    kind: task.kind,
    ci: task.ci,
    requires: task.requires,
    timeoutClass: task.timeoutClass,
    run: task.run
  };
}

function formatRun(run) {
  if (run.type === "command") return [run.command, ...run.args].join(" ");
  if (run.type === "suite") return `explicit test suite ${run.suite}`;
  if (run.type === "series") return `series: ${run.tasks.join(" -> ")}`;
  if (run.type === "pipeline") return `pipeline: ${run.pipeline}`;
  return run.type;
}

function printUnknown(args) {
  const requested = args.filter((arg) => !arg.startsWith("-")).join(":");
  const prefix = requested ? `${requested}:` : "";
  const suggestions = listTasks().filter((task) => task.name.startsWith(prefix)).slice(0, 12);
  console.error(`Unknown task "${requested || args.join(" ")}".`);
  if (suggestions.length > 0) {
    console.error("Available under that namespace:");
    for (const task of suggestions) console.error(`  ${task.name}`);
  } else {
    console.error("Run `pnpm task list` to see available tasks.");
  }
}

function printHelp() {
  console.log(`First-party repository task CLI

Usage:
  pnpm task list [--kind <kind>] [--all] [--json]
  pnpm task explain <task> [--json]
  pnpm task <kind> <name> [-- task arguments]
  pnpm <build|test|check|assets|release> <name>

Examples:
  pnpm test world
  pnpm check source
  pnpm assets refresh world-map
  pnpm task build server-game-data`);
}

main(process.argv.slice(2)).catch((error) => {
  console.error(error instanceof Error ? error.message : error);
  process.exitCode = 1;
});
