// The VS Code extension for ReasonR: it starts the language server
// "reasonr lsp" and adds the commands "ReasonR: Format" and "ReasonR: Explain command".
import * as vscode from "vscode";
import {
  ExecuteCommandRequest,
  LanguageClient,
  LanguageClientOptions,
  ServerOptions,
} from "vscode-languageclient/node";

let client: LanguageClient | undefined;

export async function activate(context: vscode.ExtensionContext): Promise<void> {
  context.subscriptions.push(
    vscode.commands.registerCommand("reasonr.format", () =>
      vscode.commands.executeCommand("editor.action.formatDocument"),
    ),
    vscode.commands.registerCommand("reasonr.explain", explain),
    vscode.commands.registerCommand("reasonr.restart", async () => {
      await stop();
      await start(context);
    }),
    vscode.workspace.onDidChangeConfiguration(async (e) => {
      if (e.affectsConfiguration("reasonr")) {
        await stop();
        await start(context);
      }
    }),
  );
  await start(context);
}

export function deactivate(): Thenable<void> | undefined {
  return stop();
}

async function start(context: vscode.ExtensionContext): Promise<void> {
  const config = vscode.workspace.getConfiguration("reasonr");
  const command = config.get<string>("server.path") || "reasonr";
  const serverOptions: ServerOptions = { command, args: ["lsp"] };
  const clientOptions: LanguageClientOptions = {
    documentSelector: [{ scheme: "file", language: "reasonr" }],
    synchronize: {
      fileEvents: vscode.workspace.createFileSystemWatcher("**/*.rr"),
    },
  };
  client = new LanguageClient("reasonr", "ReasonR", serverOptions, clientOptions);
  try {
    await client.start();
  } catch (err) {
    client = undefined;
    vscode.window.showErrorMessage(
      `ReasonR: could not start "${command} lsp": ${err}. Set the compiler path in the reasonr.server.path setting.`,
    );
  }
}

function stop(): Thenable<void> | undefined {
  const c = client;
  client = undefined;
  return c?.stop();
}

// explain shows a description of the method or type at the cursor (reasonr explain).
async function explain(): Promise<void> {
  const editor = vscode.window.activeTextEditor;
  if (!editor || editor.document.languageId !== "reasonr") {
    return;
  }
  if (!client) {
    vscode.window.showErrorMessage("ReasonR: the language server is not running");
    return;
  }
  const uri = editor.document.uri.toString();
  const position = client.code2ProtocolConverter.asPosition(editor.selection.active);
  try {
    const text = await client.sendRequest(ExecuteCommandRequest.type, {
      command: "reasonr.server.explain",
      arguments: [uri, position],
    });
    const doc = await vscode.workspace.openTextDocument({ content: String(text), language: "plaintext" });
    await vscode.window.showTextDocument(doc, { viewColumn: vscode.ViewColumn.Beside, preview: true });
  } catch (err) {
    vscode.window.showWarningMessage(`ReasonR: ${err instanceof Error ? err.message : err}`);
  }
}
