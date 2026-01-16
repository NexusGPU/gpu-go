import * as vscode from 'vscode';
import { CLI, Worker } from '../cli/cli';
import { AuthManager } from '../auth/authManager';

export class WorkerTreeItem extends vscode.TreeItem {
    constructor(
        public readonly worker: Worker,
        public readonly collapsibleState: vscode.TreeItemCollapsibleState
    ) {
        super(worker.name || worker.workerId, collapsibleState);
        
        this.tooltip = `Worker: ${worker.name}\nID: ${worker.workerId}\nStatus: ${worker.status}`;
        this.description = worker.status;
        
        // Set icon based on status
        if (worker.status === 'running' || worker.status === 'online') {
            this.iconPath = new vscode.ThemeIcon('broadcast', new vscode.ThemeColor('charts.green'));
            this.contextValue = 'worker-running';
        } else if (worker.status === 'offline' || worker.status === 'stopped') {
            this.iconPath = new vscode.ThemeIcon('broadcast', new vscode.ThemeColor('charts.red'));
            this.contextValue = 'worker-stopped';
        } else {
            this.iconPath = new vscode.ThemeIcon('loading~spin');
            this.contextValue = 'worker-pending';
        }

        // Make clickable to open details
        this.command = {
            command: 'gpugo.openWorkerDetails',
            title: 'Open Worker Details',
            arguments: [{ workerId: worker.workerId }]
        };
    }

    get workerId(): string {
        return this.worker.workerId;
    }
}

export class ConnectionTreeItem extends vscode.TreeItem {
    constructor(clientIp: string, connectedAt: string) {
        super(clientIp, vscode.TreeItemCollapsibleState.None);
        this.iconPath = new vscode.ThemeIcon('plug');
        this.description = connectedAt;
        this.tooltip = `Connected from ${clientIp} at ${connectedAt}`;
    }
}

export class WorkerPropertyItem extends vscode.TreeItem {
    constructor(label: string, value: string) {
        super(`${label}: ${value}`, vscode.TreeItemCollapsibleState.None);
    }
}

export class WorkersTreeProvider implements vscode.TreeDataProvider<vscode.TreeItem> {
    private _onDidChangeTreeData: vscode.EventEmitter<vscode.TreeItem | undefined | null | void> = new vscode.EventEmitter<vscode.TreeItem | undefined | null | void>();
    readonly onDidChangeTreeData: vscode.Event<vscode.TreeItem | undefined | null | void> = this._onDidChangeTreeData.event;

    private workers: Worker[] = [];
    private cli: CLI;
    private authManager: AuthManager;

    constructor(cli: CLI, authManager: AuthManager) {
        this.cli = cli;
        this.authManager = authManager;
    }

    refresh(): void {
        this._onDidChangeTreeData.fire();
    }

    getTreeItem(element: vscode.TreeItem): vscode.TreeItem {
        return element;
    }

    async getChildren(element?: vscode.TreeItem): Promise<vscode.TreeItem[]> {
        if (!this.authManager.isLoggedIn) {
            return [this.createLoginItem()];
        }

        if (!element) {
            // Root level - show workers
            try {
                this.workers = await this.cli.workerList();
                
                if (this.workers.length === 0) {
                    return [this.createEmptyItem()];
                }

                return this.workers.map(worker => 
                    new WorkerTreeItem(worker, vscode.TreeItemCollapsibleState.Collapsed)
                );
            } catch (error) {
                return [this.createErrorItem(error)];
            }
        }

        if (element instanceof WorkerTreeItem) {
            // Show worker details and connections
            const worker = element.worker;
            const items: vscode.TreeItem[] = [
                new WorkerPropertyItem('ID', worker.workerId),
                new WorkerPropertyItem('Port', String(worker.listenPort)),
                new WorkerPropertyItem('Enabled', worker.enabled ? 'Yes' : 'No')
            ];

            if (worker.gpuIds && worker.gpuIds.length > 0) {
                items.push(new WorkerPropertyItem('GPUs', worker.gpuIds.join(', ')));
            }

            // Add connections section
            if (worker.connections && worker.connections.length > 0) {
                const connectionsHeader = new vscode.TreeItem('Connections', vscode.TreeItemCollapsibleState.Expanded);
                connectionsHeader.iconPath = new vscode.ThemeIcon('plug');
                items.push(connectionsHeader);

                for (const conn of worker.connections) {
                    items.push(new ConnectionTreeItem(conn.clientIp, conn.connectedAt));
                }
            }

            return items;
        }

        return [];
    }

    private createLoginItem(): vscode.TreeItem {
        const item = new vscode.TreeItem('Login to GPU Go', vscode.TreeItemCollapsibleState.None);
        item.iconPath = new vscode.ThemeIcon('account');
        item.command = {
            command: 'gpugo.login',
            title: 'Login'
        };
        return item;
    }

    private createEmptyItem(): vscode.TreeItem {
        const item = new vscode.TreeItem('No workers found', vscode.TreeItemCollapsibleState.None);
        item.iconPath = new vscode.ThemeIcon('info');
        item.description = 'Create one from your GPU server';
        return item;
    }

    private createErrorItem(error: unknown): vscode.TreeItem {
        const item = new vscode.TreeItem('Error loading workers', vscode.TreeItemCollapsibleState.None);
        item.iconPath = new vscode.ThemeIcon('error');
        item.tooltip = String(error);
        return item;
    }
}
