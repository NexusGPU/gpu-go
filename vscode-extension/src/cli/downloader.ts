import * as vscode from 'vscode';
import * as os from 'os';
import * as path from 'path';
import * as fs from 'fs';
import * as https from 'https';
import * as http from 'http';
import { exec as execCallback } from 'child_process';
import { promisify } from 'util';
import { Logger } from '../logger';

const mkdir = promisify(fs.mkdir);
const chmod = promisify(fs.chmod);
const access = promisify(fs.access);
const exec = promisify(execCallback);
const writeFile = promisify(fs.writeFile);

export interface DownloadOptions {
    baseUrl: string;
    version: string;
}

interface PlatformInfo {
    platform: string;
    arch: string;
    ext: string;
}

export class CLIDownloader {
    constructor(private context: vscode.ExtensionContext) { }

    /**
     * Detect the current platform and architecture for binary selection.
     */
    private detectPlatformArch(): PlatformInfo {
        const platformMap: Record<string, string> = {
            'win32': 'windows',
            'darwin': 'darwin',
            'linux': 'linux'
        };
        const archMap: Record<string, string> = {
            'x64': 'amd64',
            'arm64': 'arm64',
            'ia32': '386'
        };
        const platform = os.platform();

        return {
            platform: platformMap[platform] || platform,
            arch: archMap[os.arch()] || os.arch(),
            ext: platform === 'win32' ? '.exe' : ''
        };
    }

    /**
     * Get the cache directory for CLI binaries.
     * Uses ~/.gpugo/cache for cross-instance sharing.
     */
    private getCacheDir(): string {
        return path.join(os.homedir(), '.gpugo', 'cache');
    }

    /**
     * Get the full path to the CLI binary.
     */
    getCliPath(): string {
        const { ext } = this.detectPlatformArch();
        return path.join(this.getCacheDir(), `ggo${ext}`);
    }

    /**
     * Get the path to the version file.
     */
    private getVersionPath(): string {
        return path.join(this.getCacheDir(), 'version');
    }

    /**
     * Check if CLI binary is already downloaded and executable.
     */
    async isCliDownloaded(): Promise<boolean> {
        try {
            await access(this.getCliPath(), fs.constants.X_OK);
            return true;
        } catch {
            return false;
        }
    }

    /**
     * Download a file from URL to target path, following redirects.
     */
    private downloadFile(url: string, targetPath: string): Promise<void> {
        return new Promise((resolve, reject) => {
            const client = url.startsWith('https') ? https : http;

            client.get(url, (response) => {
                // Handle redirects
                if (response.statusCode === 301 || response.statusCode === 302) {
                    const redirectUrl = response.headers.location;
                    if (redirectUrl) {
                        return this.downloadFile(redirectUrl, targetPath)
                            .then(resolve)
                            .catch(reject);
                    }
                }

                if (response.statusCode !== 200) {
                    return reject(new Error(`Failed to download: HTTP ${response.statusCode}`));
                }

                const fileStream = fs.createWriteStream(targetPath);
                response.pipe(fileStream);

                fileStream.on('finish', () => {
                    fileStream.close();
                    resolve();
                });

                fileStream.on('error', (err) => {
                    fs.unlink(targetPath, () => { /* ignore cleanup errors */ });
                    reject(err);
                });
            }).on('error', reject);
        });
    }

    /**
     * Download and install the CLI binary.
     */
    async downloadCli(options?: Partial<DownloadOptions>): Promise<string> {
        const config = vscode.workspace.getConfiguration('gpugo');
        const baseUrl = options?.baseUrl || config.get<string>(
            'cliDownloadUrl',
            'https://github.com/NexusGPU/gpu-go/releases/latest/download'
        );
        const version = options?.version || config.get<string>('cliVersion', 'latest');

        const { platform, arch, ext } = this.detectPlatformArch();
        const binaryName = `ggo-${platform}-${arch}${ext}`;
        const cliPath = this.getCliPath();

        // Build download URL
        const downloadUrl = version === 'latest'
            ? `${baseUrl}/${binaryName}`
            : `${baseUrl.replace('/latest', `/${version}`)}/${binaryName}`;

        Logger.log(`Downloading CLI from: ${downloadUrl}`);
        Logger.log(`Target path: ${cliPath}`);
        Logger.log(`Platform: ${platform}, Arch: ${arch}`);

        try {
            // Ensure cache directory exists
            await mkdir(path.dirname(cliPath), { recursive: true });

            // Download with progress notification
            await vscode.window.withProgress({
                location: vscode.ProgressLocation.Notification,
                title: 'GPUGo',
                cancellable: false
            }, async (progress) => {
                progress.report({ message: `Downloading CLI (${version})...` });
                await this.downloadFile(downloadUrl, cliPath);
                progress.report({ message: 'Setting permissions...' });
            });

            // Make executable on Unix systems
            if (platform !== 'windows') {
                await chmod(cliPath, 0o755);
            }

            // Record version for future reference
            await writeFile(this.getVersionPath(), version, 'utf8');

            Logger.log(`CLI downloaded successfully to: ${cliPath}`);
            vscode.window.showInformationMessage('GPUGo CLI downloaded successfully.');
            return cliPath;
        } catch (error) {
            const errorMessage = error instanceof Error ? error.message : String(error);
            const userMessage = `Failed to download GPUGo CLI: ${errorMessage}`;
            Logger.error('CLI download failed:', error);

            vscode.window.showErrorMessage(userMessage, 'Open Settings').then(selection => {
                if (selection === 'Open Settings') {
                    vscode.commands.executeCommand('workbench.action.openSettings', 'gpugo.cliPath');
                }
            });

            throw new Error(userMessage);
        }
    }

    /**
     * Ensure CLI is available - check custom path, PATH, cache, or download.
     *
     * Resolution order:
     * 1. Custom path from settings (gpugo.cliPath)
     * 2. 'ggo' in system PATH
     * 3. Previously downloaded binary in cache
     * 4. Auto-download (if enabled)
     */
    async ensureCliAvailable(): Promise<string> {
        const config = vscode.workspace.getConfiguration('gpugo');
        const customPath = config.get<string>('cliPath');

        // 1. Custom Path (User Setting)
        if (customPath) {
            Logger.log(`Using custom CLI path: ${customPath}`);
            return customPath;
        }

        // 2. Check system PATH
        try {
            await exec('ggo --version');
            Logger.log('CLI found in system PATH');
            return 'ggo';
        } catch {
            Logger.log('CLI not found in PATH, checking cache...');
        }

        // 3. Check existing download in cache
        if (await this.isCliDownloaded()) {
            const cliPath = this.getCliPath();
            Logger.log(`Using cached CLI at: ${cliPath}`);
            return cliPath;
        }

        // 4. Auto-download if enabled
        if (config.get<boolean>('autoDownloadCli', true)) {
            Logger.log('Auto-downloading CLI...');
            return this.downloadCli();
        }

        // 5. CLI not available - prompt user
        const message = 'GPUGo CLI not found. Please download or configure manually.';
        Logger.log(message);

        vscode.window.showWarningMessage(message, 'Download', 'Open Settings').then(selection => {
            if (selection === 'Download') {
                this.downloadCli().catch(err => Logger.error('Manual download failed:', err));
            } else if (selection === 'Open Settings') {
                vscode.commands.executeCommand('workbench.action.openSettings', 'gpugo.cliPath');
            }
        });

        throw new Error(message);
    }
}
