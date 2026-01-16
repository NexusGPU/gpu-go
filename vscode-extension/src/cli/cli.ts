import * as vscode from 'vscode';
import { spawn, exec } from 'child_process';
import * as path from 'path';
import * as os from 'os';
import { CLIDownloader } from './downloader';

export interface StudioEnv {
    name: string;
    id: string;
    mode: string;
    status: string;
    image: string;
    sshHost?: string;
    sshPort?: number;
}

export interface Worker {
    workerId: string;
    agentId: string;
    name: string;
    status: string;
    gpuIds: string[];
    listenPort: number;
    enabled: boolean;
    connections: Connection[];
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
        
        return new Promise((resolve, reject) => {
            const childProcess = spawn(cliPath, args, {
                env: { ...process.env },
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

    private async execCommandJson<T>(args: string[]): Promise<T> {
        const output = await this.execCommand([...args, '--output', 'json']);
        return JSON.parse(output) as T;
    }

    // Auth commands
    async login(token: string): Promise<void> {
        await this.execCommand(['login', '--token', token]);
    }

    async logout(): Promise<void> {
        await this.execCommand(['logout', '--force']);
    }

    async authStatus(): Promise<AuthStatus> {
        try {
            const output = await this.execCommand(['auth', 'status']);
            const loggedIn = output.includes('Logged in');
            return { loggedIn };
        } catch {
            return { loggedIn: false };
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

    // Studio commands
    async studioList(): Promise<StudioEnv[]> {
        try {
            const output = await this.execCommand(['studio', 'list']);
            return this.parseStudioListOutput(output);
        } catch (error) {
            console.error('Failed to list studios:', error);
            return [];
        }
    }

    private parseStudioListOutput(output: string): StudioEnv[] {
        const lines = output.trim().split('\n');
        const studios: StudioEnv[] = [];

        // Skip header line
        for (let i = 1; i < lines.length; i++) {
            const line = lines[i].trim();
            if (!line) continue;

            const parts = line.split(/\s{2,}/);
            if (parts.length >= 5) {
                const sshParts = parts[5]?.split(':');
                studios.push({
                    name: parts[0],
                    id: parts[1],
                    mode: parts[2],
                    status: parts[3],
                    image: parts[4],
                    sshHost: sshParts?.[0] !== '-' ? sshParts?.[0] : undefined,
                    sshPort: sshParts?.[1] ? parseInt(sshParts[1]) : undefined
                });
            }
        }

        return studios;
    }

    async studioCreate(name: string, options: {
        mode?: string;
        image?: string;
        gpuUrl?: string;
        ports?: string[];
        volumes?: string[];
        envs?: string[];
    }): Promise<void> {
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

        await this.execCommand(args);
    }

    async studioStart(name: string): Promise<void> {
        await this.execCommand(['studio', 'start', name]);
    }

    async studioStop(name: string): Promise<void> {
        await this.execCommand(['studio', 'stop', name]);
    }

    async studioRemove(name: string): Promise<void> {
        await this.execCommand(['studio', 'rm', name]);
    }

    async studioBackends(): Promise<string[]> {
        try {
            const output = await this.execCommand(['studio', 'backends']);
            const lines = output.split('\n');
            const backends: string[] = [];
            for (const line of lines) {
                const match = line.match(/^\s*-\s*(\w+)/);
                if (match) {
                    backends.push(match[1]);
                }
            }
            return backends;
        } catch {
            return [];
        }
    }

    async studioImages(): Promise<{ name: string; tag: string; description: string }[]> {
        try {
            const output = await this.execCommand(['studio', 'images']);
            const lines = output.trim().split('\n');
            const images: { name: string; tag: string; description: string }[] = [];

            // Skip header
            for (let i = 1; i < lines.length; i++) {
                const parts = lines[i].split(/\t+/);
                if (parts.length >= 2) {
                    const [nameTag, description] = parts;
                    const [name, tag] = nameTag.split(':');
                    images.push({ name, tag: tag || 'latest', description: description || '' });
                }
            }

            return images;
        } catch {
            return [];
        }
    }

    // Worker commands
    async workerList(): Promise<Worker[]> {
        try {
            const output = await this.execCommand(['worker', 'list']);
            return this.parseWorkerListOutput(output);
        } catch (error) {
            console.error('Failed to list workers:', error);
            return [];
        }
    }

    private parseWorkerListOutput(output: string): Worker[] {
        const lines = output.trim().split('\n');
        const workers: Worker[] = [];

        // Skip header and separator lines
        for (let i = 2; i < lines.length; i++) {
            const line = lines[i].trim();
            if (!line || line.startsWith('-')) continue;

            const parts = line.split(/\s{2,}/);
            if (parts.length >= 5) {
                workers.push({
                    workerId: parts[0],
                    name: parts[1],
                    status: parts[2],
                    listenPort: parseInt(parts[3]) || 0,
                    enabled: parts[4]?.toLowerCase() === 'yes',
                    agentId: '',
                    gpuIds: [],
                    connections: []
                });
            }
        }

        return workers;
    }

    async workerGet(workerId: string): Promise<Worker | null> {
        try {
            const output = await this.execCommand(['worker', 'get', workerId]);
            return this.parseWorkerGetOutput(output);
        } catch {
            return null;
        }
    }

    private parseWorkerGetOutput(output: string): Worker {
        const worker: Worker = {
            workerId: '',
            agentId: '',
            name: '',
            status: '',
            gpuIds: [],
            listenPort: 0,
            enabled: false,
            connections: []
        };

        const lines = output.split('\n');
        for (const line of lines) {
            const [key, ...valueParts] = line.split(':');
            const value = valueParts.join(':').trim();
            
            switch (key.trim().toLowerCase()) {
                case 'worker id':
                    worker.workerId = value;
                    break;
                case 'name':
                    worker.name = value;
                    break;
                case 'agent id':
                    worker.agentId = value;
                    break;
                case 'status':
                    worker.status = value;
                    break;
                case 'listen port':
                    worker.listenPort = parseInt(value) || 0;
                    break;
                case 'enabled':
                    worker.enabled = value.toLowerCase() === 'true';
                    break;
                case 'gpu ids':
                    worker.gpuIds = value.replace(/[\[\]]/g, '').split(/\s+/).filter(Boolean);
                    break;
            }
        }

        return worker;
    }

    // Agent/Device commands (uses worker list with agent info)
    async agentList(): Promise<Agent[]> {
        // For now, we'll parse the API response through CLI
        // This might need a dedicated CLI command in the future
        try {
            const output = await this.execCommand(['worker', 'list', '--verbose']);
            // Parse agent information from verbose output
            return [];
        } catch {
            return [];
        }
    }

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
        const config = vscode.workspace.getConfiguration('gpugo');
        return config.get<string>('serverUrl', 'https://api.gpu.tf');
    }

    getDashboardUrl(): string {
        const config = vscode.workspace.getConfiguration('gpugo');
        return config.get<string>('dashboardUrl', 'https://go.tensor-fusion.ai');
    }
}
