import { build } from "esbuild";
import { mkdir, readFile, readdir, writeFile } from "node:fs/promises";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const packageInfo = JSON.parse(await readFile(join(root, "package.json"), "utf8"));
const manifest = JSON.parse(await readFile(join(root, "plugin.json"), "utf8"));
if (packageInfo.name !== manifest.name || packageInfo.version !== manifest.version) {
  throw new Error("Plugin and package identity/version must match before building.");
}
const outdir = join(root, "bundle");
await mkdir(outdir, { recursive: true });
const common = { absWorkingDir: root, bundle: true, minify: true, legalComments: "none", metafile: true };
const server = await build({ ...common, entryPoints: ["src/main.ts"], outfile: "bundle/server.cjs", platform: "node", format: "cjs", target: "node24" });
const ui = await build({ ...common, entryPoints: ["src/ui/index.tsx"], outfile: "bundle/activity.js", platform: "browser", format: "iife", target: "es2022", write: false, define: { "process.env.NODE_ENV": '"production"' } });
const js = ui.outputFiles.find(file => file.path.endsWith(".js")).text.replaceAll("</script", "<\\/script");
const css = ui.outputFiles.find(file => file.path.endsWith(".css"))?.text ?? "";
await writeFile(join(outdir, "board.html"), `<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>BigBoard activity</title><style>${css}</style></head><body><div id="root"></div><script>${js}</script></body></html>\n`);

const packages = new Set();
for (const input of [...Object.keys(server.metafile.inputs), ...Object.keys(ui.metafile.inputs)]) {
  const match = input.match(/^(node_modules\/(?:@[^/]+\/)?[^/]+)\//);
  if (match) packages.add(match[1]);
}
const licenses = [];
for (const directory of [...packages].sort()) {
  const packageRoot = join(root, directory);
  const info = JSON.parse(await readFile(join(packageRoot, "package.json"), "utf8"));
  const names = (await readdir(packageRoot)).filter(name => /^licen[sc]e(?:\.|$)/i.test(name));
  if (names.length === 0) throw new Error(`Missing bundled license: ${info.name}`);
  const texts = await Promise.all(names.sort().map(name => readFile(join(packageRoot, name), "utf8")));
  licenses.push(`${info.name} ${info.version}\n${texts.join("\n")}`);
}
await writeFile(join(outdir, "THIRD_PARTY_LICENSES.txt"), licenses.join("\n\n--------------------\n\n") + "\n");
console.log("Built self-contained server, MCP App HTML, and bundled licenses.");
