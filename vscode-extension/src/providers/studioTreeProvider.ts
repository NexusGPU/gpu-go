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
        
        this.tooltip = `${env.name}\nStatus: ${env.status}\nImage: ${env.image}`;
        this.description = `${env.status} - ${env.mode}`;
        
        // Set icon based on status
        if (env.status === 'running') {
            this.iconPath = new vscode.ThemeIcon('vm-running', new vscode.ThemeColor('charts.green'));
            this.contextValue = 'studio-running';
        } else if (env.status === 'stopped' || env.status === 'exited') {
            this.iconPath = new vscode.ThemeIcon('vm-outline');
            this.contextValue = 'studio-stopped';
        } else {
            this.iconPath = new vscode.ThemeIcon('loading~spin');
            this.contextValue = 'studio-pending';
        }
    }
}

export class StudioPropertyItem extends vscode.TreeItem {
    constructor(label: string, value: string) {
        super(`${label}: ${value}`, vscode.TreeItemCollapsibleState.None);
        this.description = '';
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
            const items: vscode.TreeItem[] = [
                new StudioPropertyItem('ID', env.id),
                new StudioPropertyItem('Mode', env.mode),
                new StudioPropertyItem('Image', env.image),
                new StudioPropertyItem('Status', env.status)
            ];

            if (env.sshHost && env.sshPort) {
                items.push(new StudioPropertyItem('SSH', `${env.sshHost}:${env.sshPort}`));
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
