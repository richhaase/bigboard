import { App } from "@modelcontextprotocol/ext-apps";
import { APP_VERSION, type SelectionContext } from "../shared";
import type { BoardBridge, ToolResult } from "./controller";

export class McpBoardBridge implements BoardBridge {
  private app = new App({ name: "BigBoard", version: APP_VERSION }, {});

  async connect(onResult: (result: ToolResult) => void, onTeardown?: () => void): Promise<void> {
    this.app.ontoolresult = onResult;
    this.app.onteardown = async () => {
      onTeardown?.();
      return {};
    };
    this.app.onhostcontextchanged = (context) => {
      if (context.theme) document.documentElement.dataset.theme = context.theme;
    };
    await this.app.connect();
    const context = this.app.getHostContext();
    if (context?.theme) document.documentElement.dataset.theme = context.theme;
  }

  callTool(name: string, args: Record<string, unknown>): Promise<ToolResult> {
    return this.app.callServerTool({ name, arguments: args }) as Promise<ToolResult>;
  }

  async updateContext(context: SelectionContext): Promise<void> {
    await this.app.updateModelContext({
      content: [{ type: "text", text: `BigBoard selected scope (synthetic fixture data):\n${JSON.stringify(context)}` }],
      structuredContent: { ...context },
    });
  }

  async sendMessage(text: string): Promise<void> {
    const result = await this.app.sendMessage({ role: "user", content: [{ type: "text", text }] });
    if (result.isError) throw new Error("The conversation did not accept the selection. Try again.");
  }

  async close(): Promise<void> {
    await this.app.close();
  }
}
