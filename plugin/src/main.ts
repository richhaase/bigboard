import { readFileSync } from "node:fs";
import { join } from "node:path";
import { StdioServerTransport } from "@modelcontextprotocol/sdk/server/stdio.js";
import { BoardStore } from "./core";
import { createBigboardServer } from "./server";

const store = new BoardStore();
const server = createBigboardServer(readFileSync(join(__dirname, "board.html"), "utf8"), store);

async function stop(): Promise<void> {
  await server.close();
  store.close();
}

process.once("SIGINT", () => { void stop(); });
process.once("SIGTERM", () => { void stop(); });
void server.connect(new StdioServerTransport()).catch(() => {
  process.stderr.write("BigBoard could not start its MCP connection.\n");
  store.close();
  process.exitCode = 1;
});
