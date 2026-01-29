import * as vscode from 'vscode';
import { CLI, Agent, GPU } from '../cli/cli';
import { AuthManager } from '../auth/authManager';
import { PropertyItem, createLoginItem, createEmptyItem, createActionItem, getStatusIcon, getStatusContext } from './treeUtils';

export class AgentTreeItem extends vscode.TreeItem {
    constructor(
        public readonly agent: Agent,
        public readonly collapsibleState: vscode.TreeItemCollapsibleState
    ) {
        super(agent.hostname, collapsibleState);
        
        this.tooltip = `Host: ${agent.hostname}\nStatus: ${agent.status}\nOS: ${agent.os}/${agent.arch}`;
        this.description = `${agent.status} - ${agent.os}`;
        
        // Set icon and context based on status
        this.iconPath = getStatusIcon(agent.status, 'agent');
        this.contextValue = getStatusContext(agent.status, 'agent');
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

// Re-export PropertyItem for backwards compatibility
export { PropertyItem as DevicePropertyItem };

export class DevicesTreeProvider implements vscode.TreeDataProvider<vscode.TreeItem> {
    private _onDidChangeTreeData = new vscode.EventEmitter<vscode.TreeItem | undefined | null | void>();
    readonly onDidChangeTreeData = this._onDidChangeTreeData.event;

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
            return [createLoginItem()];
        }

        if (!element) {
            // Root level - show agents/hosts
            try {
                this.agents = await this.cli.agentList();
                
                if (this.agents.length === 0) {
                    // Show placeholder with instructions
                    return [
                        createEmptyItem('No GPU devices found', 'Add GPU servers to get started'),
                        createActionItem('Add GPU Server', 'gpugo.createWorker', 'add', 'Click to learn how to add GPU servers')
                    ];
                }

                return this.agents.map(agent => 
                    new AgentTreeItem(agent, vscode.TreeItemCollapsibleState.Collapsed)
                );
            } catch {
                // If no agents, show helpful message
                return [
                    createEmptyItem('No GPU devices found', 'Add GPU servers to get started'),
                    createActionItem('Add GPU Server', 'gpugo.createWorker', 'add', 'Click to learn how to add GPU servers')
                ];
            }
        }

        if (element instanceof AgentTreeItem) {
            // Show GPUs for this agent
            const agent = element.agent;
            const items: vscode.TreeItem[] = [];

            // Agent info
            items.push(new PropertyItem('Agent ID', agent.agentId.substring(0, 8) + '...'));
            items.push(new PropertyItem('OS', `${agent.os}/${agent.arch}`));
            
            if (agent.networkIps && agent.networkIps.length > 0) {
                items.push(new PropertyItem('IP', agent.networkIps[0]));
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
                items.push(new PropertyItem('Workers', String(agent.workers.length)));
            }

            return items;
        }

        return [];
    }
}
