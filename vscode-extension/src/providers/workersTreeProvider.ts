import * as vscode from 'vscode';
import { CLI, Worker } from '../cli/cli';
import { AuthManager } from '../auth/authManager';
import { PropertyItem, createLoginItem, createEmptyItem, createErrorItem, getStatusIcon, getStatusContext } from './treeUtils';
import { Logger } from '../logger';

export class WorkerTreeItem extends vscode.TreeItem {
    constructor(
        public readonly worker: Worker,
        public readonly collapsibleState: vscode.TreeItemCollapsibleState
    ) {
        super(worker.name || worker.workerId, collapsibleState);
        
        this.tooltip = `vGPU worker: ${worker.name}\nID: ${worker.workerId}\nStatus: ${worker.status}`;
        this.description = worker.status;
        
        // Set icon and context based on status
        this.iconPath = getStatusIcon(worker.status, 'worker');
        this.contextValue = getStatusContext(worker.status, 'worker');

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

// Re-export PropertyItem for backwards compatibility
export { PropertyItem as WorkerPropertyItem };

export class WorkersTreeProvider implements vscode.TreeDataProvider<vscode.TreeItem> {
    private _onDidChangeTreeData = new vscode.EventEmitter<vscode.TreeItem | undefined | null | void>();
    readonly onDidChangeTreeData = this._onDidChangeTreeData.event;

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
            return [createLoginItem()];
        }

        if (!element) {
            // Root level - show workers
            try {
                Logger.log('Fetching workers...');
                this.workers = await this.cli.workerList();
                Logger.log(`Found ${this.workers.length} workers`);
                
                if (this.workers.length === 0) {
                    return [createEmptyItem('No vGPU workers found', 'Create one from your GPU server')];
                }

                return this.workers.map(worker => 
                    new WorkerTreeItem(worker, vscode.TreeItemCollapsibleState.Collapsed)
                );
            } catch (error) {
                Logger.error('Error fetching workers:', error);
                return [createErrorItem('Error loading workers', error)];
            }
        }

        if (element instanceof WorkerTreeItem) {
            // Show worker details and connections
            const worker = element.worker;
            const items: vscode.TreeItem[] = [
                new PropertyItem('vGPU worker ID', worker.workerId),
                new PropertyItem('Listen Port', String(worker.listenPort)),
                new PropertyItem('Enabled', worker.enabled ? 'Yes' : 'No')
            ];

            if (worker.gpuIds && worker.gpuIds.length > 0) {
                items.push(new PropertyItem('vGPU IDs', worker.gpuIds.join(', ')));
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
}
