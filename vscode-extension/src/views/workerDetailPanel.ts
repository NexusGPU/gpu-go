import * as vscode from 'vscode';
import { CLI, Worker } from '../cli/cli';
import { getWebviewContent } from './webviewUtils';

export class WorkerDetailPanel {
    public static currentPanel: WorkerDetailPanel | undefined;
    public static readonly viewType = 'gpugo.workerDetail';

    private readonly _panel: vscode.WebviewPanel;
    private readonly _extensionUri: vscode.Uri;
    private readonly _cli: CLI;
    private readonly _workerId: string;
    private _disposables: vscode.Disposable[] = [];

    public static createOrShow(extensionUri: vscode.Uri, cli: CLI, workerId: string) {
        const column = vscode.window.activeTextEditor
            ? vscode.window.activeTextEditor.viewColumn
            : undefined;

        // If panel exists, show it
        if (WorkerDetailPanel.currentPanel) {
            WorkerDetailPanel.currentPanel._panel.reveal(column);
            WorkerDetailPanel.currentPanel.update(workerId);
            return;
        }

        // Create new panel
        const panel = vscode.window.createWebviewPanel(
            WorkerDetailPanel.viewType,
            'vGPU Worker Details',
            column || vscode.ViewColumn.One,
            {
                enableScripts: true
            }
        );

        WorkerDetailPanel.currentPanel = new WorkerDetailPanel(panel, extensionUri, cli, workerId);
    }

    private constructor(panel: vscode.WebviewPanel, extensionUri: vscode.Uri, cli: CLI, workerId: string) {
        this._panel = panel;
        this._extensionUri = extensionUri;
        this._cli = cli;
        this._workerId = workerId;

        // Set initial content
        this.update(workerId);

        // Handle disposal
        this._panel.onDidDispose(() => this.dispose(), null, this._disposables);

        // Handle messages from webview
        this._panel.webview.onDidReceiveMessage(
            async message => {
                switch (message.command) {
                    case 'refresh':
                        this.update(this._workerId);
                        break;
                    case 'openDashboard':
                        vscode.env.openExternal(vscode.Uri.parse(this._cli.getDashboardUrl() + '/workers/' + this._workerId));
                        break;
                }
            },
            null,
            this._disposables
        );
    }

    private async update(workerId: string) {
        const worker = await this._cli.workerGet(workerId);
        this._panel.title = worker ? `vGPU Worker: ${worker.name}` : 'vGPU Worker Details';
        this._panel.webview.html = this.getHtmlForWebview(worker);
    }

    private getHtmlForWebview(worker: Worker | null): string {
        const webview = this._panel.webview;

        // Note: vscode-elements URIs can be added here if needed:
        // webview.asWebviewUri(vscode.Uri.joinPath(this._extensionUri, 'node_modules', '@vscode-elements', 'elements', 'dist', 'bundled.js'))
        // webview.asWebviewUri(vscode.Uri.joinPath(this._extensionUri, 'node_modules', '@vscode', 'codicons', 'dist', 'codicon.css'))

        const nonce = getNonce();

        if (!worker) {
            return getWebviewContent(webview, this._extensionUri, nonce, `
                <h1>vGPU Worker Not Found</h1>
                <p>Unable to load vGPU worker details.</p>
            `);
        }

        const statusColor = worker.status === 'running' || worker.status === 'online' ? 'var(--vscode-charts-green)' : 'var(--vscode-charts-red)';
        
        const connectionsHtml = worker.connections && worker.connections.length > 0
            ? worker.connections.map(conn => `
                <vscode-table-row>
                    <vscode-table-cell>${conn.clientIp}</vscode-table-cell>
                    <vscode-table-cell>${conn.connectedAt}</vscode-table-cell>
                </vscode-table-row>
            `).join('')
            : '<vscode-table-row><vscode-table-cell colspan="2">No active connections</vscode-table-cell></vscode-table-row>';

        const content = `
            <div class="header">
                <h1><vscode-icon name="broadcast"></vscode-icon> ${worker.name}</h1>
                <vscode-badge style="background: ${statusColor};">${worker.status}</vscode-badge>
            </div>

            <vscode-divider></vscode-divider>

            <vscode-form-container>
                <vscode-form-group variant="vertical">
                    <vscode-label>vGPU worker ID</vscode-label>
                    <vscode-textfield readonly value="${worker.workerId}"></vscode-textfield>
                </vscode-form-group>

                <vscode-form-group variant="vertical">
                    <vscode-label>Machine Agent ID</vscode-label>
                    <vscode-textfield readonly value="${worker.agentId || 'N/A'}"></vscode-textfield>
                </vscode-form-group>

                <vscode-form-group variant="vertical">
                    <vscode-label>Listen Port</vscode-label>
                    <vscode-textfield readonly value="${worker.listenPort}"></vscode-textfield>
                </vscode-form-group>

                <vscode-form-group variant="vertical">
                    <vscode-label>vGPU IDs</vscode-label>
                    <vscode-textfield readonly value="${worker.gpuIds?.join(', ') || 'None'}"></vscode-textfield>
                </vscode-form-group>

                <vscode-form-group variant="vertical">
                    <vscode-label>Enabled</vscode-label>
                    <vscode-checkbox ${worker.enabled ? 'checked' : ''} disabled>Enabled</vscode-checkbox>
                </vscode-form-group>
            </vscode-form-container>

            <vscode-divider></vscode-divider>

            <h2><vscode-icon name="plug"></vscode-icon> Active Connections</h2>
            <vscode-table>
                <vscode-table-header slot="header">
                    <vscode-table-header-cell>Client IP</vscode-table-header-cell>
                    <vscode-table-header-cell>Connected At</vscode-table-header-cell>
                </vscode-table-header>
                <vscode-table-body slot="body">
                    ${connectionsHtml}
                </vscode-table-body>
            </vscode-table>

            <vscode-divider></vscode-divider>

            <div class="actions">
                <vscode-button id="refresh-btn">
                    <vscode-icon name="refresh" slot="start"></vscode-icon>
                    Refresh
                </vscode-button>
                <vscode-button id="dashboard-btn" secondary>
                    <vscode-icon name="link-external" slot="start"></vscode-icon>
                    Open in Dashboard
                </vscode-button>
            </div>

            <script nonce="${nonce}">
                const vscode = acquireVsCodeApi();
                
                document.getElementById('refresh-btn').addEventListener('click', () => {
                    vscode.postMessage({ command: 'refresh' });
                });

                document.getElementById('dashboard-btn').addEventListener('click', () => {
                    vscode.postMessage({ command: 'openDashboard' });
                });
            </script>
        `;

        return getWebviewContent(webview, this._extensionUri, nonce, content);
    }

    public dispose() {
        WorkerDetailPanel.currentPanel = undefined;
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
