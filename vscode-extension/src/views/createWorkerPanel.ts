import * as vscode from 'vscode';
import { CLI } from '../cli/cli';
import { getWebviewContent } from './webviewUtils';

export class CreateWorkerPanel {
    public static currentPanel: CreateWorkerPanel | undefined;
    public static readonly viewType = 'gpugo.createWorker';

    private readonly _panel: vscode.WebviewPanel;
    private readonly _extensionUri: vscode.Uri;
    private readonly _cli: CLI;
    private _disposables: vscode.Disposable[] = [];

    public static createOrShow(extensionUri: vscode.Uri, cli: CLI) {
        const column = vscode.window.activeTextEditor
            ? vscode.window.activeTextEditor.viewColumn
            : undefined;

        // If panel exists, show it
        if (CreateWorkerPanel.currentPanel) {
            CreateWorkerPanel.currentPanel._panel.reveal(column);
            return;
        }

        // Create new panel
        const panel = vscode.window.createWebviewPanel(
            CreateWorkerPanel.viewType,
            'Create vGPU Worker',
            column || vscode.ViewColumn.One,
            {
                enableScripts: true
            }
        );

        CreateWorkerPanel.currentPanel = new CreateWorkerPanel(panel, extensionUri, cli);
    }

    private constructor(panel: vscode.WebviewPanel, extensionUri: vscode.Uri, cli: CLI) {
        this._panel = panel;
        this._extensionUri = extensionUri;
        this._cli = cli;

        // Set content
        this._panel.webview.html = this.getHtmlForWebview();

        // Handle disposal
        this._panel.onDidDispose(() => this.dispose(), null, this._disposables);

        // Handle messages
        this._panel.webview.onDidReceiveMessage(
            async message => {
                switch (message.command) {
                    case 'copyCommand':
                        await vscode.env.clipboard.writeText(message.text);
                        vscode.window.showInformationMessage('Command copied to clipboard!');
                        break;
                    case 'openDashboard':
                        vscode.env.openExternal(vscode.Uri.parse(this._cli.getDashboardUrl() + '/workers/new'));
                        break;
                    case 'openTerminal': {
                        const terminal = vscode.window.createTerminal('GPUGo vGPU Worker');
                        terminal.show();
                        terminal.sendText('# Run this on your Machine Host:');
                        terminal.sendText('# ggo worker create --agent-id <agent-id> --name my-worker --gpu-ids 0');
                        break;
                    }
                }
            },
            null,
            this._disposables
        );
    }

    private getHtmlForWebview(): string {
        const webview = this._panel.webview;
        const nonce = getNonce();

        // Dashboard URL available if needed: this._cli.getDashboardUrl()

        const content = `
            <div class="header">
                <h1><vscode-icon name="add"></vscode-icon> Create vGPU Worker</h1>
            </div>

            <p class="description">
                vGPU workers let you share GPU resources from your Machine Host with remote development environments.
                Choose one of the options below to create a new vGPU worker.
            </p>

            <vscode-divider></vscode-divider>

            <div class="options-container">
                <vscode-collapsible title="Option 1: Using CLI (Recommended)" open>
                    <div class="option-content">
                        <p>Run the following command on your Machine Host to create a vGPU worker:</p>
                        
                        <div class="code-block">
                            <code>ggo worker create --agent-id &lt;agent-id&gt; --name &lt;worker-name&gt; --gpu-ids &lt;gpu-ids&gt;</code>
                            <vscode-button id="copy-cmd-btn" appearance="icon" title="Copy command">
                                <vscode-icon name="copy"></vscode-icon>
                            </vscode-button>
                        </div>

                        <vscode-divider></vscode-divider>

                        <h3>Prerequisites</h3>
                        <ul>
                            <li>GPU server with NVIDIA GPU(s)</li>
                            <li>ggo CLI installed on the server</li>
                            <li>Machine Agent running on the host (<code>ggo agent start</code>)</li>
                        </ul>

                        <h3>Parameters</h3>
                        <vscode-table zebra>
                            <vscode-table-header slot="header">
                                <vscode-table-header-cell>Parameter</vscode-table-header-cell>
                                <vscode-table-header-cell>Description</vscode-table-header-cell>
                            </vscode-table-header>
                            <vscode-table-body slot="body">
                                <vscode-table-row>
                                    <vscode-table-cell><code>--agent-id</code></vscode-table-cell>
                                    <vscode-table-cell>The Machine Agent ID from the host</vscode-table-cell>
                                </vscode-table-row>
                                <vscode-table-row>
                                    <vscode-table-cell><code>--name</code></vscode-table-cell>
                                    <vscode-table-cell>A friendly name for the vGPU worker</vscode-table-cell>
                                </vscode-table-row>
                                <vscode-table-row>
                                    <vscode-table-cell><code>--gpu-ids</code></vscode-table-cell>
                                    <vscode-table-cell>Comma-separated GPU IDs (e.g., "0,1")</vscode-table-cell>
                                </vscode-table-row>
                                <vscode-table-row>
                                    <vscode-table-cell><code>--port</code></vscode-table-cell>
                                    <vscode-table-cell>Listen port (default: 9001)</vscode-table-cell>
                                </vscode-table-row>
                            </vscode-table-body>
                        </vscode-table>

                        <div class="actions" style="margin-top: 16px;">
                            <vscode-button id="terminal-btn">
                                <vscode-icon name="terminal" slot="start"></vscode-icon>
                                Open Terminal
                            </vscode-button>
                        </div>
                    </div>
                </vscode-collapsible>

                <vscode-collapsible title="Option 2: Using Dashboard">
                    <div class="option-content">
                        <p>Create and manage vGPU workers through the web dashboard with a graphical interface.</p>
                        
                        <div class="actions" style="margin-top: 16px;">
                            <vscode-button id="dashboard-btn">
                                <vscode-icon name="link-external" slot="start"></vscode-icon>
                                Open Dashboard
                            </vscode-button>
                        </div>
                    </div>
                </vscode-collapsible>
            </div>

            <vscode-divider></vscode-divider>

            <div class="info-box">
                <vscode-icon name="lightbulb"></vscode-icon>
                <div>
                    <strong>Quick Start Guide</strong>
                    <ol>
                        <li>Install <code>ggo</code> on your Machine Host</li>
                        <li>Run <code>ggo login</code> to authenticate</li>
                        <li>Run <code>ggo agent start</code> to start the Machine Agent</li>
                        <li>Create vGPU workers with <code>ggo worker create</code></li>
                        <li>Use vGPU workers in your studio environments</li>
                    </ol>
                </div>
            </div>

            <script nonce="${nonce}">
                const vscode = acquireVsCodeApi();
                
                document.getElementById('copy-cmd-btn').addEventListener('click', () => {
                    vscode.postMessage({ 
                        command: 'copyCommand',
                        text: 'ggo worker create --agent-id <agent-id> --name <worker-name> --gpu-ids <gpu-ids>'
                    });
                });

                document.getElementById('dashboard-btn').addEventListener('click', () => {
                    vscode.postMessage({ command: 'openDashboard' });
                });

                document.getElementById('terminal-btn').addEventListener('click', () => {
                    vscode.postMessage({ command: 'openTerminal' });
                });
            </script>
        `;

        return getWebviewContent(webview, this._extensionUri, nonce, content);
    }

    public dispose() {
        CreateWorkerPanel.currentPanel = undefined;
        this._panel.dispose();
        while (this._disposables.length) {
            const d = this._disposables.pop();
            if (d) {
                d.dispose();
            }
        }
    }
}

function getNonce() {
    let text = '';
    const possible = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789';
    for (let i = 0; i < 32; i++) {
        text += possible.charAt(Math.floor(Math.random() * possible.length));
    }
    return text;
}
