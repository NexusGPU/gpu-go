import * as vscode from 'vscode';
import { spawn } from 'child_process';
import * as path from 'path';
import * as os from 'os';
import { CLIDownloader } from './downloader';
import { Logger } from '../logger';

// =============================================================================
// JSON Response Types (snake_case from CLI)
// =============================================================================

interface ListResponse<T> {
    items: T[];
    total: number;
}

interface DetailResponse<T> {
    item: T;
}

interface ActionResponse {
    success: boolean;
    message: string;
    id?: string;
}

interface StudioEnvJSON {
    name: string;
    id: string;
    mode: string;
    status: string;
    image: string;
    ssh_host?: string;
    ssh_port?: number;
    ssh_user?: string;
    gpu_worker_url?: string;
}

interface WorkerJSON {
    worker_id: string;
    agent_id?: string;
    name: string;
    status: string;
    gpu_ids?: string[];
    listen_port: number;
    enabled: boolean;
    is_default?: boolean;
    connections?: ConnectionJSON[];
    created_at?: string;
}

interface ConnectionJSON {
    client_ip: string;
    connected_at: string;
}

interface ShareJSON {
    share_id: string;
    short_code: string;
    short_link: string;
    worker_id: string;
    hardware_vendor: string;
    connection_url: string;
    expires_at?: string;
    max_uses?: number;
    used_count: number;
    created_at: string;
}

interface GPUJSON {
    gpu_id: string;
    vendor: string;
    model: string;
    vram_mb: number;
    driver_version?: string;
    cuda_version?: string;
}

interface AuthStatusJSON {
    logged_in: boolean;
    token?: string;
    created_at?: string;
    expires_at?: string;
}

interface AgentStatusJSON {
    registered: boolean;
    agent_id?: string;
    config_version?: number;
    server_url?: string;
    gpus?: GPUJSON[];
    workers?: WorkerJSON[];
}

// =============================================================================
// Domain Types (camelCase for TypeScript)
// =============================================================================

export interface StudioEnv {
    name: string;
    id: string;
    mode: string;
    status: string;
    image: string;
    sshHost?: string;
    sshPort?: number;
    sshUser?: string;
    gpuWorkerUrl?: string;
    ports?: string[];
}

export interface Worker {
    workerId: string;
    agentId: string;
    name: string;
    status: string;
    gpuIds: string[];
    listenPort: number;
    enabled: boolean;
    isDefault?: boolean;
    connections: Connection[];
    createdAt?: string;
}

export interface Connection {
    clientIp: string;
    connectedAt: string;
}

export interface Agent {
    agentId: string;
    hostname: string;
    status: string;
    os: string;
    arch: string;
    networkIps: string[];
    gpus: GPU[];
    workers: Worker[];
    lastSeenAt: string;
}

export interface GPU {
    gpuId: string;
    vendor: string;
    model: string;
    vramMb: number;
    driverVersion?: string;
    cudaVersion?: string;
}

export interface AuthStatus {
    loggedIn: boolean;
    token?: string;
    createdAt?: string;
    expiresAt?: string;
}

export interface Share {
    shareId: string;
    shortCode: string;
    shortLink: string;
    workerId: string;
    hardwareVendor: string;
    connectionUrl: string;
    expiresAt?: string;
    maxUses?: number;
    usedCount: number;
    createdAt: string;
}

export interface AgentStatus {
    registered: boolean;
    agentId?: string;
    configVersion?: number;
    serverUrl?: string;
    gpus?: GPU[];
    workers?: Worker[];
}

// =============================================================================
// Options Types
// =============================================================================

export interface StudioCreateOptions {
    mode?: string;
    image?: string;
    gpuUrl?: string;
    ports?: string[];
    volumes?: string[];
    envs?: string[];
}

export interface WorkerCreateOptions {
    agentId: string;
    name: string;
    gpuIds: string[];
    port?: number;
    enabled?: boolean;
}

export interface WorkerUpdateOptions {
    name?: string;
    gpuIds?: string[];
    port?: number;
    enabled?: boolean;
}

export interface ShareCreateOptions {
    expiresIn?: string;
    maxUses?: number;
}

// =============================================================================
// CLI Class
// =============================================================================

export class CLI {
    private downloader: CLIDownloader;
    private cliPath: string | null = null;

    constructor(private context: vscode.ExtensionContext) {
        this.downloader = new CLIDownloader(context);
    }

    async initialize(): Promise<void> {
        try {
            this.cliPath = await this.downloader.ensureCliAvailable();
            Logger.log(`CLI initialized at: ${this.cliPath}`);
        } catch (error) {
            Logger.error('Failed to initialize CLI:', error);
            this.cliPath = null;

            vscode.window.showErrorMessage(
                'GPU Go CLI initialization failed. Please check your settings or install manually.',
                'Settings',
                'Retry'
            ).then(selection => {
                if (selection === 'Settings') {
                    vscode.commands.executeCommand('workbench.action.openSettings', 'gpugo.cliPath');
                } else if (selection === 'Retry') {
                    this.initialize();
                }
            });
        }
    }

    // -------------------------------------------------------------------------
    // Internal Helpers
    // -------------------------------------------------------------------------

    private getCliPath(): string {
        if (this.cliPath) {
            return this.cliPath;
        }
        const config = vscode.workspace.getConfiguration('gpugo');
        return config.get<string>('cliPath') || 'ggo';
    }

    getServerUrl(): string {
        if (process.env.GPU_GO_ENDPOINT) {
            return process.env.GPU_GO_ENDPOINT;
        }
        return vscode.workspace.getConfiguration('gpugo').get<string>('serverUrl', 'https://go.gpu.tf');
    }

    getDashboardUrl(): string {
        return this.getServerUrl();
    }

    getTokenPath(): string {
        return path.join(os.homedir(), '.gpugo', 'token.json');
    }

    private async execCommand(args: string[]): Promise<string> {
        const cliPath = this.getCliPath();
        const serverUrl = this.getServerUrl();

        // Inject server URL for commands that need it
        const serverCommands = ['worker', 'share', 'agent'];
        const finalArgs = (args.length > 0 && serverCommands.includes(args[0]))
            ? [...args, '--server', serverUrl]
            : args;

        Logger.log(`Executing: ${cliPath} ${finalArgs.join(' ')}`);

        // Resolve token
        let userToken = process.env.GPU_GO_TOKEN;
        if (!userToken) {
            try {
                const fs = await import('fs/promises');
                const tokenData = JSON.parse(await fs.readFile(this.getTokenPath(), 'utf-8'));
                userToken = tokenData.token;
            } catch {
                // Token file doesn't exist or is invalid - continue without token
            }
        }

        return new Promise((resolve, reject) => {
            const child = spawn(cliPath, finalArgs, {
                env: { ...process.env, ...(userToken ? { GPU_GO_TOKEN: userToken } : {}) },
                shell: true
            });

            let stdout = '';
            let stderr = '';

            child.stdout.on('data', (data: Buffer) => {
                stdout += data.toString();
            });

            child.stderr.on('data', (data: Buffer) => {
                stderr += data.toString();
            });

            child.on('close', (code) => {
                if (code === 0) {
                    Logger.log(`Command success: ${args[0]}`);
                    resolve(stdout);
                } else {
                    const msg = stderr || `Command failed with code ${code}`;
                    Logger.error(`Command failed (code ${code}):`, msg);

                    // Special handling for CLI not found
                    if (code === 127) {
                        vscode.window.showErrorMessage(
                            `GPU Go CLI not found at '${cliPath}'. Please check your settings.`,
                            'Settings'
                        ).then(s => {
                            if (s === 'Settings') {
                                vscode.commands.executeCommand('workbench.action.openSettings', 'gpugo.cliPath');
                            }
                        });
                    }

                    reject(new Error(msg));
                }
            });

            child.on('error', reject);
        });
    }

    private async execCommandJSON<T>(args: string[]): Promise<T> {
        try {
            const output = await this.execCommand([...args, '-o', 'json']);
            return JSON.parse(output) as T;
        } catch (error) {
            Logger.error('JSON command failed:', error);
            throw error;
        }
    }

    private async execAction(args: string[], fallbackMsg: string, id: string): Promise<ActionResponse> {
        try {
            return await this.execCommandJSON<ActionResponse>(args);
        } catch {
            await this.execCommand(args);
            return { success: true, message: fallbackMsg, id };
        }
    }

    // -------------------------------------------------------------------------
    // Auth Commands
    // -------------------------------------------------------------------------

    async login(token: string): Promise<void> {
        await this.execCommand(['login', '--token', token]);
    }

    async logout(): Promise<void> {
        await this.execCommand(['logout', '--force']);
    }

    async authStatus(): Promise<AuthStatus> {
        try {
            const res = await this.execCommandJSON<AuthStatusJSON>(['auth', 'status']);
            return {
                loggedIn: res.logged_in,
                token: res.token,
                createdAt: res.created_at,
                expiresAt: res.expires_at
            };
        } catch {
            return { loggedIn: await this.isLoggedIn() };
        }
    }

    async isLoggedIn(): Promise<boolean> {
        try {
            await (await import('fs/promises')).access(this.getTokenPath());
            return true;
        } catch {
            return false;
        }
    }

    // -------------------------------------------------------------------------
    // Studio Commands
    // -------------------------------------------------------------------------

    async studioList(): Promise<StudioEnv[]> {
        try {
            const res = await this.execCommandJSON<ListResponse<StudioEnvJSON>>(['studio', 'list']);
            return res.items.map(env => this.convertStudioEnv(env));
        } catch (error) {
            Logger.error('Failed to list studios', error);
            return [];
        }
    }

    private convertStudioEnv(json: StudioEnvJSON): StudioEnv {
        const env: StudioEnv = {
            name: json.name,
            id: json.id,
            mode: json.mode,
            status: json.status,
            image: json.image,
            sshHost: json.ssh_host,
            sshPort: json.ssh_port,
            sshUser: json.ssh_user,
            gpuWorkerUrl: json.gpu_worker_url
        };
        if (env.status === 'running') {
            env.ports = this.getDefaultPorts(env.image);
        }
        return env;
    }

    private getDefaultPorts(image: string): string[] {
        const ports: string[] = [];
        const img = image.toLowerCase();

        if (img.includes('jupyter') || img.includes('notebook')) {
            ports.push('8888:8888');
        }
        if (img.includes('tensorflow') || img.includes('torch') || img.includes('pytorch')) {
            if (!ports.includes('8888:8888')) {
                ports.push('8888:8888');
            }
            ports.push('6006:6006');
        }
        if (img.includes('rstudio') || img.includes('rocker')) {
            ports.push('8787:8787');
        }
        if (img.includes('spark')) {
            ports.push('4040:4040');
        }
        if (img.includes('tensorfusion') || img.includes('studio')) {
            if (!ports.includes('8888:8888')) {
                ports.push('8888:8888');
            }
            if (!ports.includes('6006:6006')) {
                ports.push('6006:6006');
            }
        }

        return ports;
    }

    async studioCreate(name: string, options: StudioCreateOptions): Promise<StudioEnv | null> {
        const args = ['studio', 'create', name];

        if (options.mode) {
            args.push('--mode', options.mode);
        }
        if (options.image) {
            args.push('--image', options.image);
        }
        if (options.gpuUrl) {
            args.push('--gpu-url', options.gpuUrl);
        }
        options.ports?.forEach(p => args.push('-p', p));
        options.volumes?.forEach(v => args.push('--volume', v));
        options.envs?.forEach(e => args.push('-e', e));

        try {
            const res = await this.execCommandJSON<StudioEnvJSON>(args);
            return this.convertStudioEnv(res);
        } catch {
            await this.execCommand(args);
            return null;
        }
    }

    async studioStart(name: string): Promise<ActionResponse> {
        return this.execAction(['studio', 'start', name], 'Environment started', name);
    }

    async studioStop(name: string): Promise<ActionResponse> {
        return this.execAction(['studio', 'stop', name], 'Environment stopped', name);
    }

    async studioRemove(name: string): Promise<ActionResponse> {
        return this.execAction(['studio', 'rm', name, '--force'], 'Environment removed', name);
    }

    async studioBackends(): Promise<string[]> {
        try {
            const res = await this.execCommandJSON<ListResponse<{ name: string }>>(['studio', 'backends']);
            return res.items.map(b => b.name);
        } catch {
            return [];
        }
    }

    async studioImages(): Promise<{ name: string; tag: string; description: string; features: string[] }[]> {
        try {
            const res = await this.execCommandJSON<ListResponse<{
                name: string;
                tag: string;
                description: string;
                features?: string[];
            }>>(['studio', 'images']);
            return res.items.map(img => ({
                name: img.name,
                tag: img.tag,
                description: img.description,
                features: img.features || []
            }));
        } catch {
            return [];
        }
    }

    // -------------------------------------------------------------------------
    // Worker Commands
    // -------------------------------------------------------------------------

    async workerList(): Promise<Worker[]> {
        try {
            const res = await this.execCommandJSON<ListResponse<WorkerJSON>>(['worker', 'list']);
            return res.items.map(w => this.convertWorker(w));
        } catch (error) {
            Logger.error('Failed to list workers', error);
            vscode.window.showErrorMessage(`Failed to list workers: ${error instanceof Error ? error.message : error}`);
            return [];
        }
    }

    private convertWorker(json: WorkerJSON): Worker {
        return {
            workerId: json.worker_id,
            agentId: json.agent_id || '',
            name: json.name,
            status: json.status,
            gpuIds: json.gpu_ids || [],
            listenPort: json.listen_port,
            enabled: json.enabled,
            isDefault: json.is_default,
            connections: (json.connections || []).map(c => ({
                clientIp: c.client_ip,
                connectedAt: c.connected_at
            })),
            createdAt: json.created_at
        };
    }

    async workerGet(workerId: string): Promise<Worker | null> {
        try {
            const res = await this.execCommandJSON<DetailResponse<WorkerJSON>>(['worker', 'get', workerId]);
            return this.convertWorker(res.item);
        } catch {
            return null;
        }
    }

    async workerCreate(options: WorkerCreateOptions): Promise<ActionResponse> {
        const args = [
            'worker', 'create',
            '--agent-id', options.agentId,
            '--name', options.name,
            '--gpu-ids', options.gpuIds.join(',')
        ];

        if (options.port) {
            args.push('--port', String(options.port));
        }
        if (options.enabled !== undefined) {
            args.push(options.enabled ? '--enabled' : '--disabled');
        }

        return this.execCommandJSON<ActionResponse>(args);
    }

    async workerUpdate(workerId: string, options: WorkerUpdateOptions): Promise<ActionResponse> {
        const args = ['worker', 'update', workerId];

        if (options.name) {
            args.push('--name', options.name);
        }
        if (options.gpuIds) {
            args.push('--gpu-ids', options.gpuIds.join(','));
        }
        if (options.port) {
            args.push('--port', String(options.port));
        }
        if (options.enabled !== undefined) {
            args.push(options.enabled ? '--enabled' : '--disabled');
        }

        return this.execCommandJSON<ActionResponse>(args);
    }

    async workerDelete(workerId: string): Promise<ActionResponse> {
        return this.execCommandJSON<ActionResponse>(['worker', 'delete', workerId, '--force']);
    }

    // -------------------------------------------------------------------------
    // Share Commands
    // -------------------------------------------------------------------------

    async shareList(): Promise<Share[]> {
        try {
            const res = await this.execCommandJSON<ListResponse<ShareJSON>>(['share', 'list']);
            return res.items.map(s => this.convertShare(s));
        } catch {
            return [];
        }
    }

    private convertShare(json: ShareJSON): Share {
        return {
            shareId: json.share_id,
            shortCode: json.short_code,
            shortLink: json.short_link,
            workerId: json.worker_id,
            hardwareVendor: json.hardware_vendor,
            connectionUrl: json.connection_url,
            expiresAt: json.expires_at,
            maxUses: json.max_uses,
            usedCount: json.used_count,
            createdAt: json.created_at
        };
    }

    async shareCreate(workerId: string, options?: ShareCreateOptions): Promise<Share | null> {
        const args = ['share', 'create', '--worker-id', workerId];

        if (options?.expiresIn) {
            args.push('--expires-in', options.expiresIn);
        }
        if (options?.maxUses) {
            args.push('--max-uses', String(options.maxUses));
        }

        try {
            const res = await this.execCommandJSON<ShareJSON>(args);
            return this.convertShare(res);
        } catch {
            return null;
        }
    }

    async shareDelete(shareId: string): Promise<ActionResponse> {
        return this.execCommandJSON<ActionResponse>(['share', 'delete', shareId, '--force']);
    }

    // -------------------------------------------------------------------------
    // Agent Commands
    // -------------------------------------------------------------------------

    async agentList(): Promise<Agent[]> {
        try {
            // Aggregate agents from worker list
            const workers = await this.workerList();
            const agentMap = new Map<string, Agent>();

            for (const worker of workers) {
                if (!worker.agentId) {
                    continue;
                }
                if (!agentMap.has(worker.agentId)) {
                    agentMap.set(worker.agentId, {
                        agentId: worker.agentId,
                        hostname: worker.agentId,
                        status: 'online',
                        os: '',
                        arch: '',
                        networkIps: [],
                        gpus: [],
                        workers: [],
                        lastSeenAt: ''
                    });
                }
                agentMap.get(worker.agentId)!.workers.push(worker);
            }

            return Array.from(agentMap.values());
        } catch {
            return [];
        }
    }

    async agentStatus(): Promise<AgentStatus> {
        try {
            const res = await this.execCommandJSON<AgentStatusJSON>(['agent', 'status']);
            return {
                registered: res.registered,
                agentId: res.agent_id,
                configVersion: res.config_version,
                serverUrl: res.server_url,
                gpus: res.gpus?.map(g => ({
                    gpuId: g.gpu_id,
                    vendor: g.vendor,
                    model: g.model,
                    vramMb: g.vram_mb,
                    driverVersion: g.driver_version,
                    cudaVersion: g.cuda_version
                })),
                workers: res.workers?.map(w => this.convertWorker(w))
            };
        } catch {
            return { registered: false };
        }
    }
}
