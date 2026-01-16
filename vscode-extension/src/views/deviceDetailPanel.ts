import * as vscode from 'vscode';
import { CLI, GPU } from '../cli/cli';
import { getWebviewContent } from './webviewUtils';

export class DeviceDetailPanel {
    public static currentPanel: DeviceDetailPanel | undefined;
    public static readonly viewType = 'gpugo.deviceDetail';

    private readonly _panel: vscode.WebviewPanel;
    private readonly _extensionUri: vscode.Uri;
    private readonly _cli: CLI;
    private readonly _deviceId: string;
    private _disposables: vscode.Disposable[] = [];

    public static createOrShow(extensionUri: vscode.Uri, cli: CLI, deviceId: string, gpu?: GPU) {
        const column = vscode.window.activeTextEditor
            ? vscode.window.activeTextEditor.viewColumn
            : undefined;

        // If panel exists, show it
        if (DeviceDetailPanel.currentPanel) {
            DeviceDetailPanel.currentPanel._panel.reveal(column);
            return;
        }

        // Create new panel
        const panel = vscode.window.createWebviewPanel(
            DeviceDetailPanel.viewType,
            'GPU Details',
            column || vscode.ViewColumn.One,
            {
                enableScripts: true
            }
        );

        DeviceDetailPanel.currentPanel = new DeviceDetailPanel(panel, extensionUri, cli, deviceId, gpu);
    }

    private constructor(panel: vscode.WebviewPanel, extensionUri: vscode.Uri, cli: CLI, deviceId: string, gpu?: GPU) {
        this._panel = panel;
        this._extensionUri = extensionUri;
        this._cli = cli;
        this._deviceId = deviceId;

        // Set content
        this._panel.webview.html = this.getHtmlForWebview(gpu);
        this._panel.title = gpu ? `GPU: ${gpu.model}` : 'GPU Details';

        // Handle disposal
        this._panel.onDidDispose(() => this.dispose(), null, this._disposables);

        // Handle messages
        this._panel.webview.onDidReceiveMessage(
            async message => {
                switch (message.command) {
                    case 'openDashboard':
                        vscode.env.openExternal(vscode.Uri.parse(this._cli.getDashboardUrl() + '/devices'));
                        break;
                }
            },
            null,
            this._disposables
        );
    }

    private getHtmlForWebview(gpu?: GPU): string {
        const webview = this._panel.webview;
        const nonce = getNonce();

        if (!gpu) {
            return getWebviewContent(webview, this._extensionUri, nonce, `
                <h1>GPU Not Found</h1>
                <p>Unable to load GPU details.</p>
            `);
        }

        const vramGb = (gpu.vramMb / 1024).toFixed(1);

        const content = `
            <div class="header">
                <h1><vscode-icon name="circuit-board"></vscode-icon> ${gpu.model}</h1>
                <vscode-badge>${gpu.vendor}</vscode-badge>
            </div>

            <vscode-divider></vscode-divider>

            <vscode-form-container>
                <vscode-form-group variant="vertical">
                    <vscode-label>GPU ID</vscode-label>
                    <vscode-textfield readonly value="${gpu.gpuId}"></vscode-textfield>
                </vscode-form-group>

                <vscode-form-group variant="vertical">
                    <vscode-label>Vendor</vscode-label>
                    <vscode-textfield readonly value="${gpu.vendor}"></vscode-textfield>
                </vscode-form-group>

                <vscode-form-group variant="vertical">
                    <vscode-label>Model</vscode-label>
                    <vscode-textfield readonly value="${gpu.model}"></vscode-textfield>
                </vscode-form-group>

                <vscode-form-group variant="vertical">
                    <vscode-label>VRAM</vscode-label>
                    <vscode-textfield readonly value="${vramGb} GB (${gpu.vramMb} MB)"></vscode-textfield>
                </vscode-form-group>

                ${gpu.driverVersion ? `
                <vscode-form-group variant="vertical">
                    <vscode-label>Driver Version</vscode-label>
                    <vscode-textfield readonly value="${gpu.driverVersion}"></vscode-textfield>
                </vscode-form-group>
                ` : ''}

                ${gpu.cudaVersion ? `
                <vscode-form-group variant="vertical">
                    <vscode-label>CUDA Version</vscode-label>
                    <vscode-textfield readonly value="${gpu.cudaVersion}"></vscode-textfield>
                </vscode-form-group>
                ` : ''}
            </vscode-form-container>

            <vscode-divider></vscode-divider>

            <div class="info-box">
                <vscode-icon name="info"></vscode-icon>
                <p>This GPU can be used to create workers for remote AI/ML workloads. 
                Create a worker to share this GPU with your development environments.</p>
            </div>

            <div class="actions">
                <vscode-button id="dashboard-btn">
                    <vscode-icon name="link-external" slot="start"></vscode-icon>
                    Open in Dashboard
                </vscode-button>
            </div>

            <script nonce="${nonce}">
                const vscode = acquireVsCodeApi();
                
                document.getElementById('dashboard-btn').addEventListener('click', () => {
                    vscode.postMessage({ command: 'openDashboard' });
                });
            </script>
        `;

        return getWebviewContent(webview, this._extensionUri, nonce, content);
    }

    public dispose() {
        DeviceDetailPanel.currentPanel = undefined;
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
