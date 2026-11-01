import { commandTask, pipelineTask, seriesTask, suiteTask } from "./define.mjs";
import { SUITES } from "../test/suites/index.mjs";

const assetSuites = new Set([
  "assets",
  "cif",
  "region",
  "world"
]);

export const TEST_TASKS = [
  pipelineTask({
    name: "test",
    description: "Run the complete deterministic test pipeline",
    kind: "test",
    ci: true,
    requires: ["client-extraction"],
    timeoutClass: "long",
    pipeline: "tests"
  }),
  ...Object.values(SUITES).map((suite) =>
    suiteTask({
      name: `test:${suite.name}`,
      description: suite.description,
      kind: "test",
      ci: true,
      requires: assetSuites.has(suite.name) ? ["generated-assets"] : [],
      timeoutClass: suite.name === "mission" || suite.name === "assets" ? "long" : "medium",
      suite: suite.name
    })
  ),
];
