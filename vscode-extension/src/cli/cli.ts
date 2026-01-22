import * as vscode from 'vscode';
import { spawn } from 'child_process';
import * as path from 'path';
import * as os from 'os';
import { CLIDownloader } from './downloader';

// JSON response types from CLI
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

// Studio types (from JSON output)
export interface StudioEnvJSON {
    name: string;
    id: string;
    mode: string;
    status: string;
    image: string;
    ssh_host?: string;
    ssh_port?: number;
    ssh_user?: string;
    gpu_worker_url?: string;
    created_at?: string;
}

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
    /** Mapped ports from container (host:container format) */
    ports?: string[];
}

export interface StudioImageJSON {
    name: string;
    tag: string;
    description: string;
    features: string[];
    size: string;
    registry: string;
}

export interface StudioBackendJSON {
    name: string;
    mode: string;
}

// Worker types (from JSON output)
export interface WorkerJSON {
    worker_id: string;
    agent_id: string;
    name: string;
    status: string;
    gpu_ids: string[];
    listen_port: number;
    enabled: boolean;
    is_default?: boolean;
    connections?: ConnectionJSON[];
    created_at?: string;
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

export interface ConnectionJSON {
    client_ip: string;
    connected_at: string;
}

export interface Connection {
    clientIp: string;
    connectedAt: string;
}

// Agent types
export interface AgentJSON {
    agent_id: string;
    hostname: string;
    status: string;
    os: string;
    arch: string;
    network_ips?: string[];
    gpus?: GPUJSON[];
    workers?: WorkerJSON[];
    last_seen_at?: string;
    created_at?: string;
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

export interface GPUJSON {
    gpu_id: string;
    vendor: string;
    model: string;
    vram_mb: number;
    driver_version?: string;
    cuda_version?: string;
}

export interface GPU {
    gpuId: string;
    vendor: string;
    model: string;
    vramMb: number;
    driverVersion?: string;
    cudaVersion?: string;
}

// Auth types
export interface AuthStatusJSON {
    logged_in: boolean;
    token?: string;
    created_at?: string;
    expires_at?: string;
}

export interface AuthStatus {
    loggedIn: boolean;
    token?: string;
    createdAt?: string;
    expiresAt?: string;
}

// Share types
export interface ShareJSON {
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

export class CLI {
    private context: vscode.ExtensionContext;
    private downloader: CLIDownloader;
    private cliPath: string | null = null;

    constructor(context: vscode.ExtensionContext) {
        this.context = context;
        this.downloader = new CLIDownloader(context);
    }

    /**
     * Initialize CLI - ensure it's available
     */
    async initialize(): Promise<void> {
        try {
            this.cliPath = await this.downloader.ensureCliAvailable();
            console.log(`CLI initialized at: ${this.cliPath}`);
        } catch (error) {
            console.error('Failed to initialize CLI:', error);
            // Set to default and hope it's in PATH
            this.cliPath = 'ggo';
        }
    }

    private getCliPath(): string {
        if (this.cliPath) {
            return this.cliPath;
        }
        
        const config = vscode.workspace.getConfiguration('gpugo');
        const customPath = config.get<string>('cliPath', '');
        if (customPath) {
            return customPath;
        }
        return 'ggo';
    }

    private async execCommand(args: string[]): Promise<string> {
        const cliPath = this.getCliPath();
        const serverUrl = this.getServerUrl();
        
        // Add server URL to commands that need it
        const serverCommands = ['worker', 'share', 'agent'];
        const needsServer = args.length > 0 && serverCommands.includes(args[0]);
        const finalArgs = needsServer ? [...args, '--server', serverUrl] : args;
        
        // Get user token: prioritize GPU_GO_TOKEN env var, then fall back to token file
        let userToken = process.env.GPU_GO_TOKEN || '';
        if (!userToken) {
            try {
                const fs = await import('fs/promises');
                const tokenContent = await fs.readFile(this.getTokenPath(), 'utf-8');
                const tokenConfig = JSON.parse(tokenContent);
                userToken = tokenConfig.token || '';
            } catch {
                // Token file doesn't exist or is invalid
            }
        }
        
        return new Promise((resolve, reject) => {
            const childProcess = spawn(cliPath, finalArgs, {
                env: { 
                    ...process.env,
                    ...(userToken ? { GPU_GO_TOKEN: userToken } : {})
                },
                shell: true
            });

            let stdout = '';
            let stderr = '';

            childProcess.stdout.on('data', (data: Buffer) => {
                stdout += data.toString();
            });

            childProcess.stderr.on('data', (data: Buffer) => {
                stderr += data.toString();
            });

            childProcess.on('close', (code: number | null) => {
                if (code === 0) {
                    resolve(stdout);
                } else {
                    reject(new Error(stderr || `Command failed with code ${code}`));
                }
            });

            childProcess.on('error', (err: Error) => {
                reject(err);
            });
        });
    }

    /**
     * Execute a command with JSON output format
     */
    private async execCommandJSON<T>(args: string[]): Promise<T> {
        const output = await this.execCommand([...args, '-o', 'json']);
        try {
        return JSON.parse(output) as T;
        } catch (error) {
            console.error('Failed to parse JSON output:', output);
            throw new Error(`Failed to parse CLI output as JSON: ${error}`);
        }
    }

    // ==================== Auth commands ====================

    async login(token: string): Promise<void> {
        await this.execCommand(['login', '--token', token]);
    }

    async logout(): Promise<void> {
        await this.execCommand(['logout', '--force']);
    }

    async authStatus(): Promise<AuthStatus> {
        try {
            // Try JSON output first
            const result = await this.execCommandJSON<AuthStatusJSON>(['auth', 'status']);
            return {
                loggedIn: result.logged_in,
                token: result.token,
                createdAt: result.created_at,
                expiresAt: result.expires_at
            };
        } catch {
            // Fallback: check token file exists
            return { loggedIn: await this.isLoggedIn() };
        }
    }

    // Check if user is logged in by checking token file
    async isLoggedIn(): Promise<boolean> {
        const tokenPath = this.getTokenPath();
        try {
            const fs = await import('fs/promises');
            await fs.access(tokenPath);
            return true;
        } catch {
            return false;
        }
    }

    getTokenPath(): string {
        const homeDir = os.homedir();
        return path.join(homeDir, '.gpugo', 'token.json');
    }

    // ==================== Studio commands ====================

    async studioList(): Promise<StudioEnv[]> {
        try {
            const result = await this.execCommandJSON<ListResponse<StudioEnvJSON>>(['studio', 'list']);
            return result.items.map(env => this.convertStudioEnv(env));
        } catch (error) {
            console.error('Failed to list studios:', error);
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
                
                // Add default ports based on image type for running environments
                if (env.status === 'running') {
                    env.ports = this.getDefaultPortsForImage(env.image);
                }
                
        return env;
    }

    /**
     * Get default ports based on image type
     */
    private getDefaultPortsForImage(image: string): string[] {
        const ports: string[] = [];
        const imageLower = image.toLowerCase();
        
        // Jupyter-based images
        if (imageLower.includes('jupyter') || imageLower.includes('notebook')) {
            ports.push('8888:8888');
        }
        // TensorFlow/PyTorch images often have TensorBoard
        if (imageLower.includes('tensorflow') || imageLower.includes('torch') || imageLower.includes('pytorch')) {
            if (!ports.includes('8888:8888')) { ports.push('8888:8888'); }
            ports.push('6006:6006');
        }
        // RStudio images
        if (imageLower.includes('rstudio') || imageLower.includes('rocker')) {
            ports.push('8787:8787');
        }
        // Spark images
        if (imageLower.includes('spark')) {
            ports.push('4040:4040');
        }
        // TensorFusion images have standard ports
        if (imageLower.includes('tensorfusion') || imageLower.includes('studio')) {
            if (!ports.includes('8888:8888')) { ports.push('8888:8888'); }
            if (!ports.includes('6006:6006')) { ports.push('6006:6006'); }
        }
        
        return ports;
    }

    async studioCreate(name: string, options: {
        mode?: string;
        image?: string;
        gpuUrl?: string;
        ports?: string[];
        volumes?: string[];
        envs?: string[];
    }): Promise<StudioEnv | null> {
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
        if (options.ports) {
            for (const port of options.ports) {
                args.push('-p', port);
            }
        }
        if (options.volumes) {
            for (const vol of options.volumes) {
                args.push('--volume', vol);
            }
        }
        if (options.envs) {
            for (const env of options.envs) {
                args.push('-e', env);
            }
        }

        try {
            const result = await this.execCommandJSON<StudioEnvJSON>(args);
            return this.convertStudioEnv(result);
        } catch {
            // Fallback to non-JSON for backward compatibility
        await this.execCommand(args);
            return null;
        }
    }

    async studioStart(name: string): Promise<ActionResponse> {
        try {
            return await this.execCommandJSON<ActionResponse>(['studio', 'start', name]);
        } catch {
        await this.execCommand(['studio', 'start', name]);
            return { success: true, message: 'Environment started', id: name };
        }
    }

    async studioStop(name: string): Promise<ActionResponse> {
        try {
            return await this.execCommandJSON<ActionResponse>(['studio', 'stop', name]);
        } catch {
        await this.execCommand(['studio', 'stop', name]);
            return { success: true, message: 'Environment stopped', id: name };
        }
    }

    async studioRemove(name: string): Promise<ActionResponse> {
        try {
            return await this.execCommandJSON<ActionResponse>(['studio', 'rm', name, '--force']);
        } catch {
            await this.execCommand(['studio', 'rm', name, '--force']);
            return { success: true, message: 'Environment removed', id: name };
        }
    }

    async studioBackends(): Promise<string[]> {
        try {
            const result = await this.execCommandJSON<ListResponse<StudioBackendJSON>>(['studio', 'backends']);
            return result.items.map(b => b.name);
        } catch {
            return [];
        }
    }

    async studioImages(): Promise<{ name: string; tag: string; description: string; features: string[] }[]> {
        try {
            const result = await this.execCommandJSON<ListResponse<StudioImageJSON>>(['studio', 'images']);
            return result.items.map(img => ({
                name: img.name,
                tag: img.tag,
                description: img.description,
                features: img.features || []
            }));
        } catch {
            return [];
        }
    }

    // ==================== Worker commands ====================

    async workerList(): Promise<Worker[]> {
        try {
            const result = await this.execCommandJSON<ListResponse<WorkerJSON>>(['worker', 'list']);
            return result.items.map(w => this.convertWorker(w));
        } catch (error) {
            console.error('Failed to list workers:', error);
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
            const result = await this.execCommandJSON<DetailResponse<WorkerJSON>>(['worker', 'get', workerId]);
            return this.convertWorker(result.item);
        } catch {
            return null;
        }
    }

    async workerCreate(options: {
        agentId: string;
        name: string;
        gpuIds: string[];
        port?: number;
        enabled?: boolean;
    }): Promise<ActionResponse> {
        const args = ['worker', 'create', 
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

        return await this.execCommandJSON<ActionResponse>(args);
    }

    async workerUpdate(workerId: string, options: {
        name?: string;
        gpuIds?: string[];
        port?: number;
        enabled?: boolean;
    }): Promise<ActionResponse> {
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

        return await this.execCommandJSON<ActionResponse>(args);
    }

    async workerDelete(workerId: string): Promise<ActionResponse> {
        return await this.execCommandJSON<ActionResponse>(['worker', 'delete', workerId, '--force']);
    }

    // ==================== Share commands ====================

    async shareList(): Promise<Share[]> {
        try {
            const result = await this.execCommandJSON<ListResponse<ShareJSON>>(['share', 'list']);
            return result.items.map(s => this.convertShare(s));
        } catch (error) {
            console.error('Failed to list shares:', error);
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

    async shareCreate(workerId: string, options?: {
        expiresIn?: string;
        maxUses?: number;
    }): Promise<Share | null> {
        const args = ['share', 'create', '--worker-id', workerId];
        
        if (options?.expiresIn) {
            args.push('--expires-in', options.expiresIn);
        }
        if (options?.maxUses) {
            args.push('--max-uses', String(options.maxUses));
        }

        try {
            const result = await this.execCommandJSON<ShareJSON>(args);
            return this.convertShare(result);
        } catch (error) {
            console.error('Failed to create share:', error);
            return null;
        }
    }

    async shareDelete(shareId: string): Promise<ActionResponse> {
        return await this.execCommandJSON<ActionResponse>(['share', 'delete', shareId, '--force']);
    }

    // ==================== Agent/Device commands ====================

    async agentList(): Promise<Agent[]> {
        // Note: This requires server-side support for listing agents
        // For now, we try to get agent info through the worker list
        // which includes agent information
        try {
            const workers = await this.workerList();
            
            // Group workers by agent ID
            const agentMap = new Map<string, Agent>();
            
            for (const worker of workers) {
                if (!worker.agentId) { continue; }
                
                if (!agentMap.has(worker.agentId)) {
                    agentMap.set(worker.agentId, {
                        agentId: worker.agentId,
                        hostname: worker.agentId, // Will be updated if we get more info
                        status: 'online',
                        os: '',
                        arch: '',
                        networkIps: [],
                        gpus: [],
                        workers: [],
                        lastSeenAt: ''
                    });
                }
                
                const agent = agentMap.get(worker.agentId)!;
                agent.workers.push(worker);
            }
            
            return Array.from(agentMap.values());
        } catch (error) {
            console.error('Failed to list agents:', error);
            return [];
        }
    }

    async agentStatus(): Promise<{
        registered: boolean;
        agentId?: string;
        configVersion?: number;
        serverUrl?: string;
        gpus?: GPU[];
        workers?: Worker[];
    }> {
        try {
            const result = await this.execCommandJSON<{
                registered: boolean;
                agent_id?: string;
                config_version?: number;
                server_url?: string;
                gpus?: GPUJSON[];
                workers?: WorkerJSON[];
            }>(['agent', 'status']);
            
            return {
                registered: result.registered,
                agentId: result.agent_id,
                configVersion: result.config_version,
                serverUrl: result.server_url,
                gpus: result.gpus?.map(g => ({
                    gpuId: g.gpu_id,
                    vendor: g.vendor,
                    model: g.model,
                    vramMb: g.vram_mb,
                    driverVersion: g.driver_version,
                    cudaVersion: g.cuda_version
                })),
                workers: result.workers?.map(w => this.convertWorker(w))
            };
        } catch {
            return { registered: false };
        }
    }

    // ==================== Helper methods ====================

    // Helper to check CLI availability
    async checkCliAvailable(): Promise<boolean> {
        try {
            await this.execCommand(['--version']);
            return true;
        } catch {
            return false;
        }
    }

    // Helper to get server URL from config
    getServerUrl(): string {
        // Check environment variable first (useful for debugging/testing)
        const envEndpoint = process.env.GPU_GO_ENDPOINT;
        if (envEndpoint) {
            return envEndpoint;
        }
        const config = vscode.workspace.getConfiguration('gpugo');
        return config.get<string>('serverUrl', 'https://api.gpu.tf');
    }

    getDashboardUrl(): string {
        const config = vscode.workspace.getConfiguration('gpugo');
        return config.get<string>('dashboardUrl', 'https://go.tensor-fusion.ai');
    }
}
