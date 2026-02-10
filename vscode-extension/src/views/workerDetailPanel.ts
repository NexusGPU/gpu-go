import * as vscode from 'vscode';
import { CLI, Worker, GPU } from '../cli/cli';
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
            'vGPU Details',
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
                    case 'copyShareLink':
                        if (message.link) {
                            await vscode.env.clipboard.writeText(message.link);
                            vscode.window.showInformationMessage('Share link copied to clipboard');
                        }
                        break;
                }
            },
            null,
            this._disposables
        );
    }

    private async update(workerId: string) {
        const worker = await this._cli.workerGet(workerId);

        // Fetch GPU details from agent if we have an agentId
        let gpuDetails: GPU[] = [];
        if (worker?.agentId) {
            try {
                const agent = await this._cli.agentGet(worker.agentId);
                if (agent?.gpus) {
                    gpuDetails = agent.gpus;
                }
            } catch {
                // Fall back to no GPU details
            }
        }

        // Fetch share links for this worker
        let shareLinks: { shortLink: string; shortCode: string; usedCount: number; createdAt: string }[] = [];
        try {
            const shares = await this._cli.shareList();
            shareLinks = shares
                .filter(s => s.workerId === workerId)
                .map(s => ({
                    shortLink: s.shortLink,
                    shortCode: s.shortCode,
                    usedCount: s.usedCount,
                    createdAt: s.createdAt
                }));
        } catch {
            // Share links are optional
        }

        this._panel.title = worker ? `vGPU: ${worker.name}` : 'vGPU Details';
        this._panel.webview.html = this.getHtmlForWebview(worker, gpuDetails, shareLinks);
    }

    private getHtmlForWebview(
        worker: Worker | null,
        gpuDetails: GPU[],
        shareLinks: { shortLink: string; shortCode: string; usedCount: number; createdAt: string }[]
    ): string {
        const webview = this._panel.webview;
        const nonce = getNonce();

        if (!worker) {
            return getWebviewContent(webview, this._extensionUri, nonce, `
                <h1>vGPU Not Found</h1>
                <p>Unable to load vGPU details.</p>
            `);
        }

        const statusColor = worker.status === 'running' || worker.status === 'online' ? 'var(--vscode-charts-green)' : 'var(--vscode-charts-red)';

        // Match GPU IDs to detailed GPU info
        const matchedGpus = (worker.gpuIds || []).map(gpuId => {
            const detail = gpuDetails.find(g => g.gpuId === gpuId);
            return detail || { gpuId, vendor: '', model: '', vramMb: 0 } as GPU;
        });

        // Build GPU cards HTML
        const gpuCardsHtml = matchedGpus.length > 0 ? matchedGpus.map(gpu => {
            const vramGb = gpu.vramMb ? (gpu.vramMb / 1024).toFixed(1) : '?';
            const hasDetail = gpu.model && gpu.model !== '';
            return `
                <div class="gpu-card">
                    <div class="gpu-card-header">
                        <span class="gpu-card-icon"><vscode-icon name="circuit-board"></vscode-icon></span>
                        <span class="gpu-card-title">${hasDetail ? gpu.model : 'GPU'}</span>
                        ${gpu.vendor ? `<vscode-badge>${gpu.vendor}</vscode-badge>` : ''}
                    </div>
                    <div class="gpu-card-body">
                        <div class="gpu-card-stat">
                            <span class="gpu-stat-label">VRAM</span>
                            <span class="gpu-stat-value">${gpu.vramMb ? `${vramGb} GB` : 'N/A'}</span>
                        </div>
                        <div class="gpu-card-stat">
                            <span class="gpu-stat-label">ID</span>
                            <span class="gpu-stat-value gpu-id-value" title="${gpu.gpuId}">${gpu.gpuId.substring(0, 16)}...</span>
                        </div>
                        ${gpu.driverVersion ? `
                        <div class="gpu-card-stat">
                            <span class="gpu-stat-label">Driver</span>
                            <span class="gpu-stat-value">${gpu.driverVersion}</span>
                        </div>` : ''}
                    </div>
                </div>
            `;
        }).join('') : '<p class="description">No GPUs assigned</p>';

        const connectionsHtml = worker.connections && worker.connections.length > 0
            ? worker.connections.map(conn => `
                <vscode-table-row>
                    <vscode-table-cell>${conn.clientIp}</vscode-table-cell>
                    <vscode-table-cell>${conn.connectedAt}</vscode-table-cell>
                </vscode-table-row>
            `).join('')
            : '<vscode-table-row><vscode-table-cell colspan="2">No active connections</vscode-table-cell></vscode-table-row>';

        const shareLinksHtml = shareLinks.length > 0 ? `
            <h2><vscode-icon name="link"></vscode-icon> Share Links</h2>
            <div class="share-links">
                ${shareLinks.map(s => `
                    <div class="share-link-item">
                        <code>${s.shortLink}</code>
                        <span class="description">Used ${s.usedCount}x</span>
                        <vscode-button appearance="icon" class="copy-share-btn" data-link="${s.shortLink}" title="Copy link">
                            <vscode-icon name="copy"></vscode-icon>
                        </vscode-button>
                    </div>
                `).join('')}
            </div>
            <vscode-divider></vscode-divider>
        ` : '';

        const content = `
            <div class="header">
                <h1><vscode-icon name="broadcast"></vscode-icon> ${worker.name}</h1>
                <vscode-badge style="background: ${statusColor};">${worker.status}</vscode-badge>
            </div>

            <vscode-divider></vscode-divider>

            <div class="detail-grid">
                <div class="detail-item">
                    <span class="detail-label">Worker ID</span>
                    <span class="detail-value" title="${worker.workerId}">${worker.workerId}</span>
                </div>
                <div class="detail-item">
                    <span class="detail-label">Agent ID</span>
                    <span class="detail-value" title="${worker.agentId || 'N/A'}">${worker.agentId || 'N/A'}</span>
                </div>
                <div class="detail-item">
                    <span class="detail-label">Listen Port</span>
                    <span class="detail-value">${worker.listenPort}</span>
                </div>
                <div class="detail-item">
                    <span class="detail-label">Enabled</span>
                    <span class="detail-value">${worker.enabled ? '● Yes' : '○ No'}</span>
                </div>
            </div>

            <vscode-divider></vscode-divider>

            <h2><vscode-icon name="circuit-board"></vscode-icon> GPUs (${matchedGpus.length})</h2>
            <div class="gpu-cards">
                ${gpuCardsHtml}
            </div>

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

            ${shareLinksHtml}

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

                document.querySelectorAll('.copy-share-btn').forEach(btn => {
                    btn.addEventListener('click', () => {
                        vscode.postMessage({ command: 'copyShareLink', link: btn.getAttribute('data-link') });
                    });
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
