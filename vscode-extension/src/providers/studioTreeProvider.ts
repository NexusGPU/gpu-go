import * as vscode from 'vscode';
import { CLI, StudioEnv } from '../cli/cli';
import { AuthManager } from '../auth/authManager';

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
        if (env.status === 'running') {
            this.description = `● Running`;
            this.iconPath = new vscode.ThemeIcon('vm-running', new vscode.ThemeColor('charts.green'));
            this.contextValue = 'studio-running';
        } else if (env.status === 'stopped' || env.status === 'exited') {
            this.description = `○ Stopped`;
            this.iconPath = new vscode.ThemeIcon('vm-outline');
            this.contextValue = 'studio-stopped';
        } else {
            this.description = `◔ ${env.status}`;
            this.iconPath = new vscode.ThemeIcon('loading~spin');
            this.contextValue = 'studio-pending';
        }
    }
}

export class StudioPropertyItem extends vscode.TreeItem {
    constructor(label: string, value: string, options?: { icon?: string; command?: vscode.Command }) {
        super(`${label}: ${value}`, vscode.TreeItemCollapsibleState.None);
        this.description = '';
        if (options?.icon) {
            this.iconPath = new vscode.ThemeIcon(options.icon);
        }
        if (options?.command) {
            this.command = options.command;
        }
    }
}

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
    private _onDidChangeTreeData: vscode.EventEmitter<vscode.TreeItem | undefined | null | void> = new vscode.EventEmitter<vscode.TreeItem | undefined | null | void>();
    readonly onDidChangeTreeData: vscode.Event<vscode.TreeItem | undefined | null | void> = this._onDidChangeTreeData.event;

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
            return [this.createLoginItem()];
        }

        if (!element) {
            // Root level - show studio environments
            try {
                this.studios = await this.cli.studioList();
                
                if (this.studios.length === 0) {
                    return [this.createEmptyItem()];
                }

                return this.studios.map(env => 
                    new StudioTreeItem(env.name, env, vscode.TreeItemCollapsibleState.Collapsed)
                );
            } catch (error) {
                return [this.createErrorItem(error)];
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
                    const sshItem = new StudioPropertyItem('SSH', `ggo-${env.name}`, {
                        icon: 'terminal',
                        command: {
                            command: 'gpugo.connectStudio',
                            title: 'Connect via SSH',
                            arguments: [element]
                        }
                    });
                    items.push(sshItem);
                }
            }

            // Show basic info
            items.push(new StudioPropertyItem('ID', env.id, { icon: 'key' }));
            items.push(new StudioPropertyItem('Mode', env.mode, { icon: 'server' }));
            items.push(new StudioPropertyItem('Image', env.image, { icon: 'package' }));
            items.push(new StudioPropertyItem('Status', env.status, { icon: 'pulse' }));

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
        const item = new vscode.TreeItem('No studio environments', vscode.TreeItemCollapsibleState.None);
        item.iconPath = new vscode.ThemeIcon('info');
        item.description = 'Click + to create one';
        return item;
    }

    private createErrorItem(error: unknown): vscode.TreeItem {
        const item = new vscode.TreeItem('Error loading studios', vscode.TreeItemCollapsibleState.None);
        item.iconPath = new vscode.ThemeIcon('error');
        item.tooltip = String(error);
        return item;
    }
}
