import * as vscode from 'vscode';
import { CLI, StudioEnv } from '../cli/cli';
import { AuthManager } from '../auth/authManager';
import { PropertyItem, createLoginItem, createEmptyItem, createErrorItem, getStatusIcon, getStatusContext } from './treeUtils';

export class StudioTreeItem extends vscode.TreeItem {
    constructor(
        public readonly name: string,
        public readonly env: StudioEnv,
        public readonly collapsibleState: vscode.TreeItemCollapsibleState
    ) {
        super(name, collapsibleState);
        
        // Build rich tooltip
        let tooltip = `${env.name}\nStatus: ${env.status}\nImage: ${env.image}`;
        if (env.sshPort) {
            tooltip += `\nSSH: ssh ggo-${env.name}`;
        }
        this.tooltip = new vscode.MarkdownString(tooltip);
        
        // Show status with helpful hints
        this.iconPath = getStatusIcon(env.status, 'studio');
        this.contextValue = getStatusContext(env.status, 'studio');
        
        if (env.status === 'running') {
            this.description = `● Running`;
        } else if (env.status === 'stopped' || env.status === 'exited') {
            this.description = `○ Stopped`;
        } else if (env.status === 'unknown') {
            this.description = `○ Unknown`;
        } else {
            this.description = `◔ ${env.status}`;
        }
    }
}

// Re-export PropertyItem for backwards compatibility
export { PropertyItem as StudioPropertyItem };

export class StudioWebUrlItem extends vscode.TreeItem {
    constructor(name: string, url: string) {
        super(`🌐 ${name}`, vscode.TreeItemCollapsibleState.None);
        this.description = url;
        this.tooltip = `Click to open ${url}`;
        this.iconPath = new vscode.ThemeIcon('globe');
        this.command = {
            command: 'vscode.open',
            title: 'Open URL',
            arguments: [vscode.Uri.parse(url)]
        };
    }
}

export class StudioTreeProvider implements vscode.TreeDataProvider<vscode.TreeItem> {
    private _onDidChangeTreeData = new vscode.EventEmitter<vscode.TreeItem | undefined | null | void>();
    readonly onDidChangeTreeData = this._onDidChangeTreeData.event;

    private studios: StudioEnv[] = [];
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
            // Root level - show studio environments
            try {
                this.studios = await this.cli.studioList();
                
                if (this.studios.length === 0) {
                    return [createEmptyItem('No studio environments', 'Click + to create one')];
                }

                return this.studios.map(env => 
                    new StudioTreeItem(env.name, env, vscode.TreeItemCollapsibleState.Collapsed)
                );
            } catch (error) {
                return [createErrorItem('Error loading studios', error)];
            }
        }

        if (element instanceof StudioTreeItem) {
            // Show studio details
            const env = element.env;
            const items: vscode.TreeItem[] = [];

            // For running studios, show quick access links first
            if (env.status === 'running') {
                // Add web UI links based on known ports
                const ports = env.ports || [];
                for (const portMapping of ports) {
                    const [hostPort, containerPort] = portMapping.split(':').map(p => parseInt(p));
                    if (containerPort === 8888) {
                        items.push(new StudioWebUrlItem('Jupyter Lab', `http://localhost:${hostPort}/lab`));
                    } else if (containerPort === 6006) {
                        items.push(new StudioWebUrlItem('TensorBoard', `http://localhost:${hostPort}`));
                    } else if (containerPort === 8787) {
                        items.push(new StudioWebUrlItem('RStudio', `http://localhost:${hostPort}`));
                    } else if (containerPort === 4040) {
                        items.push(new StudioWebUrlItem('Spark UI', `http://localhost:${hostPort}`));
                    }
                }
                
                // Add SSH quick connect
                if (env.sshHost && env.sshPort) {
                    items.push(new PropertyItem('SSH', `ggo-${env.name}`, {
                        icon: 'terminal',
                        command: {
                            command: 'gpugo.connectStudio',
                            title: 'Connect via SSH',
                            arguments: [element]
                        }
                    }));
                }
            }

            // Show basic info
            items.push(new PropertyItem('ID', env.id, { icon: 'key' }));
            items.push(new PropertyItem('Mode', env.mode, { icon: 'server' }));
            items.push(new PropertyItem('Image', env.image, { icon: 'package' }));
            items.push(new PropertyItem('Status', env.status, { icon: 'pulse' }));

            return items;
        }

        return [];
    }
}
