import assert from "node:assert/strict";
import { readFile, readdir } from "node:fs/promises";
import { test } from "node:test";
import { fileURLToPath } from "node:url";
import { resolve } from "node:path";

test("the marketplace package exposes consistent bigboard skill identities without title overrides", async () => {
  const pluginRoot = fileURLToPath(new URL("../", import.meta.url));
  const repoRoot = fileURLToPath(new URL("../../", import.meta.url));
  const manifest = JSON.parse(await readFile(new URL("../plugin.json", import.meta.url), "utf8"));
  const pkg = JSON.parse(await readFile(new URL("../package.json", import.meta.url), "utf8"));
  const catalog = JSON.parse(await readFile(new URL("../../.agents/plugins/marketplace.json", import.meta.url), "utf8"));
  assert.equal(manifest.name, "bigboard");
  assert.equal(pkg.name, manifest.name);
  assert.equal(pkg.version, manifest.version);
  const entries = catalog.plugins.filter((entry: { name: string }) => entry.name === manifest.name);
  assert.equal(entries.length, 1);
  assert.equal(resolve(repoRoot, entries[0].source.path), resolve(pluginRoot));
  assert.equal(catalog.plugins.some((entry: { name: string }) => entry.name === "bigboard-mcp-app"), false);
  const skillIds: string[] = [];
  for (const directory of await readdir(new URL("../skills/", import.meta.url), { withFileTypes: true })) {
    if (!directory.isDirectory()) continue;
    const skill = new URL(`../skills/${directory.name}/`, import.meta.url);
    const source = await readFile(new URL("SKILL.md", skill), "utf8");
    const name = /^name:\s*([a-z0-9-]+)\s*$/m.exec(source)?.[1];
    assert.equal(name, directory.name);
    const id = `${manifest.name}:${name}`;
    skillIds.push(id);
    let ui: string;
    try { ui = await readFile(new URL("agents/openai.yaml", skill), "utf8"); }
    catch (error) {
      if ((error as NodeJS.ErrnoException).code === "ENOENT") continue;
      throw error;
    }
    assert.doesNotMatch(ui, /^\s*display_name\s*:/m);
    const prompt = /^\s*default_prompt:\s*"(.*)"\s*$/m.exec(ui)?.[1];
    if (prompt) assert.ok(prompt.includes(`$${id}`));
  }
  assert.deepEqual(skillIds.sort(), ["bigboard:configure", "bigboard:explore"]);
});
