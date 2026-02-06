import * as vscode from 'vscode';
import { CLI, Share, Worker } from '../cli/cli';
import { groupSharesByWorker } from '../utils/shareUtils';
import { CreateStudioPanel } from './createStudioPanel';
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
                    case 'useShare':
                        await this.useShareLink(message.shareValue);
                        break;
                    case 'createStudio':
                        CreateStudioPanel.createOrShow(this._extensionUri, this._cli, message.shareValue);
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
        const shares = await this.getWorkerShares(workerId);
        this._panel.webview.html = this.getHtmlForWebview(worker, shares);
    }

    private async getWorkerShares(workerId: string): Promise<ShareSummary[]> {
        try {
            const shares = await this._cli.shareList();
            const grouped = groupSharesByWorker(shares, 10);
            const workerShares = grouped.get(workerId) ?? [];
            return workerShares.map(share => toShareSummary(share));
        } catch {
            return [];
        }
    }

    private getHtmlForWebview(worker: Worker | null, shares: ShareSummary[]): string {
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
        const sharesJson = JSON.stringify(shares);
        const shareOptions = shares.map(share => {
            return `<vscode-option value="${share.shareId}">${share.displayLabel}</vscode-option>`;
        }).join('');
        
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

            <h2><vscode-icon name="link"></vscode-icon> Share Links</h2>
            ${shares.length === 0 ? `
                <div class="info-box">
                    <vscode-icon name="info"></vscode-icon>
                    <div>
                        <strong>No share links yet</strong>
                        <p>Create one with <code>ggo worker share</code> in the CLI.</p>
                    </div>
                </div>
            ` : `
                <vscode-form-container>
                    <vscode-form-group variant="vertical">
                        <vscode-label for="share-select">Share code</vscode-label>
                        <vscode-single-select id="share-select" name="share-select">
                            ${shareOptions}
                        </vscode-single-select>
                        <vscode-form-helper>Share links let others connect to this vGPU worker.</vscode-form-helper>
                    </vscode-form-group>
                </vscode-form-container>

                <div id="share-detail" class="info-box" style="margin-top: 16px;">
                    <vscode-icon name="rocket"></vscode-icon>
                    <div style="width: 100%;">
                        <strong id="share-detail-title">Selected share</strong>
                        <div style="margin-top: 8px; display: grid; grid-template-columns: auto 1fr; gap: 6px 12px; font-size: 0.9em;">
                            <span style="color: var(--vscode-descriptionForeground);">Connection URL (IP:port)</span>
                            <span id="share-connection"></span>
                            <span style="color: var(--vscode-descriptionForeground);">Vendor</span>
                            <span id="share-vendor"></span>
                            <span style="color: var(--vscode-descriptionForeground);">Short Link</span>
                            <span id="share-link"></span>
                            <span style="color: var(--vscode-descriptionForeground);">Created</span>
                            <span id="share-created"></span>
                        </div>

                        <div class="actions" style="margin-top: 12px;">
                            <vscode-button id="use-share-btn">
                                <vscode-icon name="terminal" slot="start"></vscode-icon>
                                Use remote vGPU
                            </vscode-button>
                            <vscode-button id="create-studio-btn" secondary>
                                <vscode-icon name="vm" slot="start"></vscode-icon>
                                Create Studio from this share
                            </vscode-button>
                        </div>
                        <p class="description" style="margin-top: 8px;">Use remote vGPU opens a terminal and runs <code>ggo use</code> for you.</p>
                    </div>
                </div>
            `}

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
                const shares = ${sharesJson};
                let selectedShare = shares[0] || null;

                function setText(id, value) {
                    const el = document.getElementById(id);
                    if (el) {
                        el.textContent = value || 'Not available';
                    }
                }

                function updateShareDetails() {
                    if (!selectedShare) {
                        return;
                    }
                    setText('share-connection', selectedShare.connectionUrl);
                    setText('share-vendor', selectedShare.hardwareVendor);
                    setText('share-link', selectedShare.shortLink);
                    setText('share-created', selectedShare.createdAt);
                }
                
                document.getElementById('refresh-btn').addEventListener('click', () => {
                    vscode.postMessage({ command: 'refresh' });
                });

                document.getElementById('dashboard-btn').addEventListener('click', () => {
                    vscode.postMessage({ command: 'openDashboard' });
                });

                const shareSelect = document.getElementById('share-select');
                if (shareSelect) {
                    shareSelect.addEventListener('change', (event) => {
                        const shareId = event.target.value;
                        selectedShare = shares.find(share => share.shareId === shareId) || null;
                        updateShareDetails();
                    });
                }

                const useShareBtn = document.getElementById('use-share-btn');
                if (useShareBtn) {
                    useShareBtn.addEventListener('click', () => {
                        if (!selectedShare) { return; }
                        vscode.postMessage({ command: 'useShare', shareValue: selectedShare.useValue });
                    });
                }

                const createStudioBtn = document.getElementById('create-studio-btn');
                if (createStudioBtn) {
                    createStudioBtn.addEventListener('click', () => {
                        if (!selectedShare) { return; }
                        vscode.postMessage({ command: 'createStudio', shareValue: selectedShare.createValue });
                    });
                }

                if (shares.length > 0 && shareSelect) {
                    shareSelect.value = shares[0].shareId;
                    updateShareDetails();
                }
            </script>
        `;

        return getWebviewContent(webview, this._extensionUri, nonce, content);
    }

    private async useShareLink(shareValue: string | undefined): Promise<void> {
        if (!shareValue) {
            vscode.window.showWarningMessage('No share link selected.');
            return;
        }
        if (process.platform === 'darwin') {
            vscode.window.showWarningMessage('Remote vGPU use is not supported on macOS yet.');
            return;
        }
        const terminal = vscode.window.createTerminal('GPUGo vGPU');
        terminal.show();
        terminal.sendText(`ggo use ${shareValue} -y`);
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

interface ShareSummary {
    shareId: string;
    shortCode: string;
    shortLink: string;
    connectionUrl: string;
    hardwareVendor: string;
    createdAt: string;
    displayLabel: string;
    useValue: string;
    createValue: string;
}

function toShareSummary(share: Share): ShareSummary {
    const displayLabel = share.shortCode || share.shortLink || share.shareId;
    const useValue = share.shortCode || share.shortLink || share.shareId;
    const createValue = share.shortLink || share.shortCode || share.shareId;
    return {
        shareId: share.shareId,
        shortCode: share.shortCode,
        shortLink: share.shortLink,
        connectionUrl: share.connectionUrl,
        hardwareVendor: share.hardwareVendor,
        createdAt: share.createdAt || 'Not available',
        displayLabel,
        useValue,
        createValue
    };
}
