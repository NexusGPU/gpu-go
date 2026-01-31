import * as vscode from 'vscode';
import { AuthManager } from './auth/authManager';
import { StudioTreeProvider } from './providers/studioTreeProvider';
import { WorkersTreeProvider } from './providers/workersTreeProvider';
import { DevicesTreeProvider } from './providers/devicesTreeProvider';
import { WorkerDetailPanel } from './views/workerDetailPanel';
import { DeviceDetailPanel } from './views/deviceDetailPanel';
import { CreateWorkerPanel } from './views/createWorkerPanel';
import { CreateStudioPanel } from './views/createStudioPanel';
import { CLI } from './cli/cli';
import { Logger } from './logger';

let authManager: AuthManager;
let studioProvider: StudioTreeProvider;
let workersProvider: WorkersTreeProvider;
let devicesProvider: DevicesTreeProvider;
let refreshInterval: NodeJS.Timeout | undefined;

export async function activate(context: vscode.ExtensionContext) {
    // Initialize Logger
    Logger.initialize(context, 'GPU Go');
    Logger.log('GPU Go extension is now active');

    // Initialize CLI wrapper
    const cli = new CLI(context);

    // Ensure CLI is available (auto-download if needed)
    try {
        await cli.initialize();
    } catch (error) {
        Logger.error('Failed to initialize CLI:', error);
        vscode.window.showWarningMessage(
            'GPU Go CLI could not be initialized. Some features may not work.',
            'Open Settings'
        ).then(action => {
            if (action === 'Open Settings') {
                vscode.commands.executeCommand('workbench.action.openSettings', 'gpugo');
            }
        });
    }

    // Initialize auth manager
    authManager = new AuthManager(context, cli);

    // Initialize tree providers
    studioProvider = new StudioTreeProvider(cli, authManager);
    workersProvider = new WorkersTreeProvider(cli, authManager);
    devicesProvider = new DevicesTreeProvider(cli, authManager);

    // Register tree views
    context.subscriptions.push(
        vscode.window.registerTreeDataProvider('gpugo.studio', studioProvider),
        vscode.window.registerTreeDataProvider('gpugo.workers', workersProvider),
        vscode.window.registerTreeDataProvider('gpugo.devices', devicesProvider)
    );

    // Register commands
    registerCommands(context, cli);

    // Check authentication status on startup (non-blocking)
    authManager.checkLoginStatus().then(isLoggedIn => {
        if (!isLoggedIn) {
            // Show login prompt (non-blocking)
            vscode.window.showInformationMessage(
                'Welcome to GPU Go! Please login to access your remote GPUs.',
                'Login'
            ).then(action => {
                if (action === 'Login') {
                    vscode.commands.executeCommand('gpugo.login');
                }
            });
        }
    });

    // Setup auto-refresh
    setupAutoRefresh(context);

    // Listen for configuration changes
    context.subscriptions.push(
        vscode.workspace.onDidChangeConfiguration(e => {
            if (e.affectsConfiguration('gpugo.autoRefreshInterval')) {
                setupAutoRefresh(context);
            }
        })
    );

    // Listen for auth state changes
    authManager.onAuthStateChanged(() => {
        studioProvider.refresh();
        workersProvider.refresh();
        devicesProvider.refresh();
    });
}

function registerCommands(context: vscode.ExtensionContext, cli: CLI) {
    // Auth commands
    context.subscriptions.push(
        vscode.commands.registerCommand('gpugo.login', async () => {
            await authManager.login();
        })
    );

    context.subscriptions.push(
        vscode.commands.registerCommand('gpugo.logout', async () => {
            await authManager.logout();
        })
    );

    // Refresh commands
    context.subscriptions.push(
        vscode.commands.registerCommand('gpugo.refreshStudio', () => {
            studioProvider.refresh();
        })
    );

    context.subscriptions.push(
        vscode.commands.registerCommand('gpugo.refreshWorkers', () => {
            workersProvider.refresh();
        })
    );

    context.subscriptions.push(
        vscode.commands.registerCommand('gpugo.refreshDevices', () => {
            devicesProvider.refresh();
        })
    );

    // Studio commands
    context.subscriptions.push(
        vscode.commands.registerCommand('gpugo.createStudio', async () => {
            CreateStudioPanel.createOrShow(context.extensionUri, cli);
        })
    );

    context.subscriptions.push(
        vscode.commands.registerCommand('gpugo.startStudio', async (item) => {
            if (item?.name) {
                await cli.studioStart(item.name);
                studioProvider.refresh();
                vscode.window.showInformationMessage(`Studio '${item.name}' started`);
            }
        })
    );

    context.subscriptions.push(
        vscode.commands.registerCommand('gpugo.stopStudio', async (item) => {
            if (item?.name) {
                await cli.studioStop(item.name);
                studioProvider.refresh();
                vscode.window.showInformationMessage(`Studio '${item.name}' stopped`);
            }
        })
    );

    context.subscriptions.push(
        vscode.commands.registerCommand('gpugo.removeStudio', async (item) => {
            if (item?.name) {
                const confirm = await vscode.window.showWarningMessage(
                    `Are you sure you want to remove studio '${item.name}'?`,
                    { modal: true },
                    'Remove'
                );
                if (confirm === 'Remove') {
                    await cli.studioRemove(item.name);
                    studioProvider.refresh();
                    vscode.window.showInformationMessage(`Studio '${item.name}' removed`);
                }
            }
        })
    );

    context.subscriptions.push(
        vscode.commands.registerCommand('gpugo.connectStudio', async (item) => {
            if (item?.name) {
                // Open Remote-SSH connection
                const sshHost = `ggo-${item.name}`;
                vscode.commands.executeCommand('opensshremotes.openEmptyWindow', { host: sshHost });
            }
        })
    );

    context.subscriptions.push(
        vscode.commands.registerCommand('gpugo.showStudioLogs', async (item) => {
            if (item?.name) {
                const terminal = vscode.window.createTerminal(`Studio Logs: ${item.name}`);
                terminal.show();
                terminal.sendText(`ggo studio logs -f ${item.name}`);
            }
        })
    );

    // Open Jupyter Lab in browser
    context.subscriptions.push(
        vscode.commands.registerCommand('gpugo.openJupyter', async (item) => {
            if (item?.env) {
                const port = findMappedPort(item.env.ports, 8888) || 8888;
                const url = `http://localhost:${port}/lab`;
                vscode.env.openExternal(vscode.Uri.parse(url));
                vscode.window.showInformationMessage(`Opening Jupyter Lab at ${url}`);
            }
        })
    );

    // Open TensorBoard in browser
    context.subscriptions.push(
        vscode.commands.registerCommand('gpugo.openTensorBoard', async (item) => {
            if (item?.env) {
                const port = findMappedPort(item.env.ports, 6006) || 6006;
                const url = `http://localhost:${port}`;
                vscode.env.openExternal(vscode.Uri.parse(url));
                vscode.window.showInformationMessage(`Opening TensorBoard at ${url}`);
            }
        })
    );

    // Open generic Web UI
    context.subscriptions.push(
        vscode.commands.registerCommand('gpugo.openWebUI', async (item, port?: number) => {
            if (item?.env && port) {
                const mappedPort = findMappedPort(item.env.ports, port) || port;
                const url = `http://localhost:${mappedPort}`;
                vscode.env.openExternal(vscode.Uri.parse(url));
            }
        })
    );

    // Worker commands
    context.subscriptions.push(
        vscode.commands.registerCommand('gpugo.createWorker', async () => {
            CreateWorkerPanel.createOrShow(context.extensionUri, cli);
        })
    );

    context.subscriptions.push(
        vscode.commands.registerCommand('gpugo.openWorkerDetails', async (item) => {
            if (item?.workerId) {
                WorkerDetailPanel.createOrShow(context.extensionUri, cli, item.workerId);
            }
        })
    );

    context.subscriptions.push(
        vscode.commands.registerCommand('gpugo.copyWorkerCommand', async () => {
            const command = 'ggo worker create --agent-id <agent-id> --name <worker-name> --gpu-ids <gpu-ids>';
            await vscode.env.clipboard.writeText(command);
            vscode.window.showInformationMessage('Worker create command copied to clipboard');
        })
    );

    // Device commands
    context.subscriptions.push(
        vscode.commands.registerCommand('gpugo.openDeviceDetails', async (item) => {
            if (item?.deviceId) {
                DeviceDetailPanel.createOrShow(context.extensionUri, cli, item.deviceId);
            }
        })
    );

    // Dashboard command
    context.subscriptions.push(
        vscode.commands.registerCommand('gpugo.openDashboard', async () => {
            const config = vscode.workspace.getConfiguration('gpugo');
            const dashboardUrl = config.get<string>('dashboardUrl', 'https://go.tensor-fusion.ai');
            vscode.env.openExternal(vscode.Uri.parse(dashboardUrl));
        })
    );

    // CLI download command
    context.subscriptions.push(
        vscode.commands.registerCommand('gpugo.downloadCli', async () => {
            const { CLIDownloader } = await import('./cli/downloader');
            const downloader = new CLIDownloader(context);
            await downloader.downloadCli();
        })
    );
}

function setupAutoRefresh(context: vscode.ExtensionContext) {
    // Clear existing interval
    if (refreshInterval) {
        clearInterval(refreshInterval);
        refreshInterval = undefined;
    }

    const config = vscode.workspace.getConfiguration('gpugo');
    const interval = config.get<number>('autoRefreshInterval', 30);

    if (interval > 0) {
        refreshInterval = setInterval(() => {
            studioProvider.refresh();
            workersProvider.refresh();
            devicesProvider.refresh();
        }, interval * 1000);

        context.subscriptions.push({
            dispose: () => {
                if (refreshInterval) {
                    clearInterval(refreshInterval);
                }
            }
        });
    }
}

export function deactivate() {
    if (refreshInterval) {
        clearInterval(refreshInterval);
    }
}

/**
 * Find the host port mapped to a container port
 * @param ports Array of port mappings in "hostPort:containerPort" format
 * @param containerPort The container port to find
 * @returns The host port if found, undefined otherwise
 */
function findMappedPort(ports: string[] | undefined, containerPort: number): number | undefined {
    if (!ports) { return undefined; }
    for (const mapping of ports) {
        const parts = mapping.split(':');
        if (parts.length >= 2) {
            const cPort = parseInt(parts[1]);
            if (cPort === containerPort) {
                return parseInt(parts[0]);
            }
        }
    }
    return undefined;
}
