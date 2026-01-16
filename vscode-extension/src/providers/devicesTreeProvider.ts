import * as vscode from 'vscode';
import { CLI, Agent, GPU } from '../cli/cli';
import { AuthManager } from '../auth/authManager';

export class AgentTreeItem extends vscode.TreeItem {
    constructor(
        public readonly agent: Agent,
        public readonly collapsibleState: vscode.TreeItemCollapsibleState
    ) {
        super(agent.hostname, collapsibleState);
        
        this.tooltip = `Host: ${agent.hostname}\nStatus: ${agent.status}\nOS: ${agent.os}/${agent.arch}`;
        this.description = `${agent.status} - ${agent.os}`;
        
        // Set icon based on status
        if (agent.status === 'online') {
            this.iconPath = new vscode.ThemeIcon('server', new vscode.ThemeColor('charts.green'));
            this.contextValue = 'agent-online';
        } else {
            this.iconPath = new vscode.ThemeIcon('server', new vscode.ThemeColor('charts.red'));
            this.contextValue = 'agent-offline';
        }
    }
}

export class GPUTreeItem extends vscode.TreeItem {
    constructor(
        public readonly gpu: GPU,
        public readonly deviceId: string
    ) {
        super(gpu.model, vscode.TreeItemCollapsibleState.None);
        
        const vramGb = (gpu.vramMb / 1024).toFixed(1);
        this.description = `${vramGb} GB`;
        this.tooltip = `${gpu.vendor} ${gpu.model}\nVRAM: ${vramGb} GB\nDriver: ${gpu.driverVersion || 'N/A'}\nCUDA: ${gpu.cudaVersion || 'N/A'}`;
        this.iconPath = new vscode.ThemeIcon('circuit-board', new vscode.ThemeColor('charts.yellow'));
        
        // Make clickable to open details
        this.command = {
            command: 'gpugo.openDeviceDetails',
            title: 'Open Device Details',
            arguments: [{ deviceId: deviceId, gpu: gpu }]
        };
    }
}

export class DevicePropertyItem extends vscode.TreeItem {
    constructor(label: string, value: string, icon?: vscode.ThemeIcon) {
        super(`${label}: ${value}`, vscode.TreeItemCollapsibleState.None);
        if (icon) {
            this.iconPath = icon;
        }
    }
}

export class DevicesTreeProvider implements vscode.TreeDataProvider<vscode.TreeItem> {
    private _onDidChangeTreeData: vscode.EventEmitter<vscode.TreeItem | undefined | null | void> = new vscode.EventEmitter<vscode.TreeItem | undefined | null | void>();
    readonly onDidChangeTreeData: vscode.Event<vscode.TreeItem | undefined | null | void> = this._onDidChangeTreeData.event;

    private agents: Agent[] = [];
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
            // Root level - show agents/hosts
            try {
                this.agents = await this.cli.agentList();
                
                if (this.agents.length === 0) {
                    // Show placeholder with instructions
                    return [
                        this.createEmptyItem(),
                        this.createAddDeviceItem()
                    ];
                }

                return this.agents.map(agent => 
                    new AgentTreeItem(agent, vscode.TreeItemCollapsibleState.Collapsed)
                );
            } catch (error) {
                // If no agents, show helpful message
                return [
                    this.createEmptyItem(),
                    this.createAddDeviceItem()
                ];
            }
        }

        if (element instanceof AgentTreeItem) {
            // Show GPUs for this agent
            const agent = element.agent;
            const items: vscode.TreeItem[] = [];

            // Agent info
            items.push(new DevicePropertyItem('Agent ID', agent.agentId.substring(0, 8) + '...'));
            items.push(new DevicePropertyItem('OS', `${agent.os}/${agent.arch}`));
            
            if (agent.networkIps && agent.networkIps.length > 0) {
                items.push(new DevicePropertyItem('IP', agent.networkIps[0]));
            }

            // GPUs
            if (agent.gpus && agent.gpus.length > 0) {
                const gpuHeader = new vscode.TreeItem(`GPUs (${agent.gpus.length})`, vscode.TreeItemCollapsibleState.Expanded);
                gpuHeader.iconPath = new vscode.ThemeIcon('circuit-board');
                items.push(gpuHeader);

                for (const gpu of agent.gpus) {
                    items.push(new GPUTreeItem(gpu, gpu.gpuId));
                }
            }

            // Workers
            if (agent.workers && agent.workers.length > 0) {
                items.push(new DevicePropertyItem('Workers', String(agent.workers.length)));
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
        const item = new vscode.TreeItem('No GPU devices found', vscode.TreeItemCollapsibleState.None);
        item.iconPath = new vscode.ThemeIcon('info');
        item.description = 'Add GPU servers to get started';
        return item;
    }

    private createAddDeviceItem(): vscode.TreeItem {
        const item = new vscode.TreeItem('Add GPU Server', vscode.TreeItemCollapsibleState.None);
        item.iconPath = new vscode.ThemeIcon('add');
        item.command = {
            command: 'gpugo.createWorker',
            title: 'Add GPU Server'
        };
        item.tooltip = 'Click to learn how to add GPU servers';
        return item;
    }
}
