import * as vscode from 'vscode';
import { CLI, Agent, GPU } from '../cli/cli';
import { AuthManager } from '../auth/authManager';
import { PropertyItem, createLoginItem, createEmptyItem, createActionItem, getStatusIcon, getStatusContext } from './treeUtils';
import { Logger } from '../logger';

export class AgentTreeItem extends vscode.TreeItem {
    constructor(
        public readonly agent: Agent,
        public readonly collapsibleState: vscode.TreeItemCollapsibleState
    ) {
        super(agent.hostname || agent.agentId, collapsibleState);

        const osInfo = agent.os && agent.arch ? `${agent.os}/${agent.arch}` : '';
        this.tooltip = `Host: ${agent.hostname}\nAgent ID: ${agent.agentId}\nStatus: ${agent.status}${osInfo ? `\nOS: ${osInfo}` : ''}`;
        this.description = `${agent.status}${osInfo ? ` · ${osInfo}` : ''}`;

        // Set icon and context based on status
        this.iconPath = getStatusIcon(agent.status, 'agent');
        this.contextValue = getStatusContext(agent.status, 'agent');
    }
}

export class GPUHeaderItem extends vscode.TreeItem {
    constructor(
        public readonly agentId: string,
        public readonly gpus: GPU[],
        collapsibleState: vscode.TreeItemCollapsibleState = vscode.TreeItemCollapsibleState.Expanded
    ) {
        super(`GPUs (${gpus.length})`, collapsibleState);
        this.iconPath = new vscode.ThemeIcon('circuit-board');
        this.contextValue = 'gpu-header';
    }
}

export class GPUTreeItem extends vscode.TreeItem {
    constructor(
        public readonly gpu: GPU,
        public readonly deviceId: string
    ) {
        super(gpu.model || gpu.gpuId, vscode.TreeItemCollapsibleState.None);

        const vramGb = gpu.vramMb ? (gpu.vramMb / 1024).toFixed(1) : '?';
        this.description = gpu.vramMb ? `${vramGb} GB` : '';
        this.tooltip = `${gpu.vendor || ''} ${gpu.model || gpu.gpuId}\nVRAM: ${vramGb} GB\nGPU ID: ${gpu.gpuId}\nDriver: ${gpu.driverVersion || 'N/A'}\nCUDA: ${gpu.cudaVersion || 'N/A'}`;
        this.iconPath = new vscode.ThemeIcon('circuit-board', new vscode.ThemeColor('charts.yellow'));

        // Make clickable to open details
        this.command = {
            command: 'gpugo.openDeviceDetails',
            title: 'Open GPU Details',
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
    private detailedAgents = new Map<string, Agent>();
    private cli: CLI;
    private authManager: AuthManager;

    constructor(cli: CLI, authManager: AuthManager) {
        this.cli = cli;
        this.authManager = authManager;
    }

    refresh(): void {
        this.detailedAgents.clear();
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
            // Root level - show agents/machines
            try {
                Logger.log('Fetching agents...');
                this.agents = await this.cli.agentList();
                Logger.log(`Found ${this.agents.length} agents`);

                if (this.agents.length === 0) {
                    return [
                        createEmptyItem('No machines found', 'Add a GPU host to get started'),
                        createActionItem('Add GPU Host', 'gpugo.addGpuHost', 'add', 'Click to register a GPU host via the dashboard')
                    ];
                }

                // Fetch detailed data for each agent in parallel
                const detailPromises = this.agents.map(async (agent) => {
                    try {
                        const detailed = await this.cli.agentGet(agent.agentId);
                        if (detailed) {
                            this.detailedAgents.set(agent.agentId, detailed);
                        }
                    } catch {
                        // Fall back to list data
                    }
                });
                await Promise.all(detailPromises);

                return this.agents.map(agent => {
                    const detailed = this.detailedAgents.get(agent.agentId) || agent;
                    return new AgentTreeItem(detailed, vscode.TreeItemCollapsibleState.Expanded);
                });
            } catch (error) {
                Logger.error('Error fetching agents:', error);
                return [
                    createEmptyItem('No machines found', 'Add a GPU host to get started'),
                    createActionItem('Add GPU Host', 'gpugo.addGpuHost', 'add', 'Click to register a GPU host via the dashboard')
                ];
            }
        }

        if (element instanceof AgentTreeItem) {
            const agent = this.detailedAgents.get(element.agent.agentId) || element.agent;
            const items: vscode.TreeItem[] = [];

            // Network IP
            if (agent.networkIps && agent.networkIps.length > 0) {
                items.push(new PropertyItem('IP', agent.networkIps[0], { icon: 'globe' }));
            }

            // GPUs - show as expandable header with GPU items underneath
            if (agent.gpus && agent.gpus.length > 0) {
                items.push(new GPUHeaderItem(agent.agentId, agent.gpus));
            }

            // Workers count
            if (agent.workers && agent.workers.length > 0) {
                items.push(new PropertyItem('vGPUs', String(agent.workers.length), { icon: 'broadcast' }));
            }

            return items;
        }

        if (element instanceof GPUHeaderItem) {
            // Return GPU items for the GPU header
            return element.gpus.map(gpu => new GPUTreeItem(gpu, gpu.gpuId));
        }

        return [];
    }
}
