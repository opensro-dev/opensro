export default {
  name: "world",
  description: "World formats, render ownership, scene lanes, and worker geometry contracts",
  stages: [
    {
      runner: "node",
      stripTypes: true,
      files: [
        "scripts/test/world/worldFormats.test.mjs",
        "scripts/test/world/outdoorObjectIndex.test.mjs",
        "scripts/test/world/dungeonBlockObjects.test.mjs",
      ]
    }
  ]
};
