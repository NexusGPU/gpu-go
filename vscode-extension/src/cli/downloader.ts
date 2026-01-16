import * as vscode from 'vscode';
import * as os from 'os';
import * as path from 'path';
import * as fs from 'fs';
import * as https from 'https';
import * as http from 'http';
import { exec as execCallback } from 'child_process';
import { promisify } from 'util';

const mkdir = promisify(fs.mkdir);
const chmod = promisify(fs.chmod);
const access = promisify(fs.access);
const exec = promisify(execCallback);

export interface DownloadOptions {
    baseUrl: string;
    version: string;
    targetDir: string;
}

export class CLIDownloader {
    private context: vscode.ExtensionContext;

    constructor(context: vscode.ExtensionContext) {
        this.context = context;
    }

    /**
     * Detect the current platform and architecture
     * Returns platform-arch string like 'darwin-arm64', 'linux-amd64', 'windows-amd64'
     */
    private detectPlatformArch(): { platform: string; arch: string; ext: string } {
        const platform = os.platform();
        const arch = os.arch();

        // Map Node.js arch to common naming conventions
        const archMap: Record<string, string> = {
            'x64': 'amd64',
            'arm64': 'arm64',
            'arm': 'arm',
            'ia32': '386'
        };

        // Map Node.js platform to common naming conventions
        const platformMap: Record<string, string> = {
            'darwin': 'darwin',
            'linux': 'linux',
            'win32': 'windows'
        };

        const mappedPlatform = platformMap[platform] || platform;
        const mappedArch = archMap[arch] || arch;
        const ext = platform === 'win32' ? '.exe' : '';

        return {
            platform: mappedPlatform,
            arch: mappedArch,
            ext
        };
    }

    /**
     * Get the CLI binary name based on platform
     */
    private getCliBinaryName(): string {
        const { platform, arch, ext } = this.detectPlatformArch();
        return `ggo-${platform}-${arch}${ext}`;
    }

    /**
     * Get the local path where the CLI should be stored
     */
    getCliPath(): string {
        const { ext } = this.detectPlatformArch();
        const binaryName = `ggo${ext}`;
        return path.join(this.context.globalStorageUri.fsPath, 'bin', binaryName);
    }

    /**
     * Check if CLI is already downloaded
     */
    async isCliDownloaded(): Promise<boolean> {
        const cliPath = this.getCliPath();
        try {
            await access(cliPath, fs.constants.X_OK);
            return true;
        } catch {
            return false;
        }
    }

    /**
     * Download file from URL
     */
    private downloadFile(url: string, targetPath: string): Promise<void> {
        return new Promise((resolve, reject) => {
            const client = url.startsWith('https') ? https : http;
            
            client.get(url, (response) => {
                // Handle redirects
                if (response.statusCode === 301 || response.statusCode === 302) {
                    const redirectUrl = response.headers.location;
                    if (redirectUrl) {
                        this.downloadFile(redirectUrl, targetPath).then(resolve).catch(reject);
                        return;
                    }
                }

                if (response.statusCode !== 200) {
                    reject(new Error(`Failed to download: HTTP ${response.statusCode}`));
                    return;
                }

                const fileStream = fs.createWriteStream(targetPath);
                response.pipe(fileStream);

                fileStream.on('finish', () => {
                    fileStream.close();
                    resolve();
                });

                fileStream.on('error', (err) => {
                    fs.unlink(targetPath, () => {});
                    reject(err);
                });
            }).on('error', (err) => {
                reject(err);
            });
        });
    }

    /**
     * Download and install the CLI binary
     */
    async downloadCli(options?: Partial<DownloadOptions>): Promise<string> {
        const config = vscode.workspace.getConfiguration('gpugo');
        const baseUrl = options?.baseUrl || config.get<string>('cliDownloadUrl', 'https://github.com/NexusGPU/gpu-go/releases/latest/download');
        const version = options?.version || config.get<string>('cliVersion', 'latest');

        const { platform, arch } = this.detectPlatformArch();
        const binaryName = this.getCliBinaryName();
        const cliPath = this.getCliPath();
        const binDir = path.dirname(cliPath);

        // Construct download URL
        let downloadUrl: string;
        if (version === 'latest') {
            downloadUrl = `${baseUrl}/${binaryName}`;
        } else {
            // For specific versions, adjust URL pattern as needed
            downloadUrl = `${baseUrl.replace('/latest/', `/${version}/`)}/${binaryName}`;
        }

        console.log(`Downloading CLI from: ${downloadUrl}`);
        console.log(`Platform: ${platform}, Arch: ${arch}`);
        console.log(`Target path: ${cliPath}`);

        try {
            // Create bin directory if it doesn't exist
            await mkdir(binDir, { recursive: true });

            // Download the binary
            await vscode.window.withProgress({
                location: vscode.ProgressLocation.Notification,
                title: "GPU Go",
                cancellable: false
            }, async (progress) => {
                progress.report({ message: "Downloading GPU Go CLI..." });
                await this.downloadFile(downloadUrl, cliPath);
                progress.report({ message: "Setting permissions..." });
            });

            // Make the binary executable (Unix-like systems)
            if (platform !== 'windows') {
                await chmod(cliPath, 0o755);
            }

            console.log(`CLI downloaded successfully to: ${cliPath}`);
            return cliPath;
        } catch (error) {
            const errorMessage = error instanceof Error ? error.message : String(error);
            console.error('Failed to download CLI:', errorMessage);
            throw new Error(`Failed to download GPU Go CLI: ${errorMessage}`);
        }
    }

    /**
     * Ensure CLI is available - download if needed
     */
    async ensureCliAvailable(): Promise<string> {
        const config = vscode.workspace.getConfiguration('gpugo');
        const autoDownload = config.get<boolean>('autoDownloadCli', true);
        const customPath = config.get<string>('cliPath', '');

        // If user specified a custom path, use that
        if (customPath) {
            console.log(`Using custom CLI path: ${customPath}`);
            return customPath;
        }

        // Check if CLI is already downloaded
        const isDownloaded = await this.isCliDownloaded();
        if (isDownloaded) {
            console.log('CLI already downloaded');
            return this.getCliPath();
        }

        // Try to find ggo in PATH
        try {
            await exec('ggo --version');
            console.log('CLI found in PATH');
            return 'ggo';
        } catch {
            console.log('CLI not found in PATH');
        }

        // Auto-download if enabled
        if (autoDownload) {
            try {
                const cliPath = await this.downloadCli();
                vscode.window.showInformationMessage('GPU Go CLI downloaded successfully!');
                return cliPath;
            } catch (error) {
                const errorMessage = error instanceof Error ? error.message : String(error);
                const action = await vscode.window.showErrorMessage(
                    `Failed to download GPU Go CLI: ${errorMessage}`,
                    'Manual Setup',
                    'Retry'
                );

                if (action === 'Retry') {
                    return await this.downloadCli();
                } else if (action === 'Manual Setup') {
                    const infoMessage = 'Please install GPU Go CLI manually and configure the path in settings.';
                    await vscode.window.showInformationMessage(infoMessage, 'Open Settings');
                    vscode.commands.executeCommand('workbench.action.openSettings', 'gpugo.cliPath');
                }
                throw error;
            }
        } else {
            const action = await vscode.window.showWarningMessage(
                'GPU Go CLI not found. Would you like to download it?',
                'Download',
                'Manual Setup'
            );

            if (action === 'Download') {
                return await this.downloadCli();
            } else if (action === 'Manual Setup') {
                const infoMessage = 'Please install GPU Go CLI manually and configure the path in settings.';
                await vscode.window.showInformationMessage(infoMessage, 'Open Settings');
                vscode.commands.executeCommand('workbench.action.openSettings', 'gpugo.cliPath');
            }
            throw new Error('CLI not available');
        }
    }

    /**
     * Manually trigger CLI download (can be exposed as a command)
     */
    async manualDownload(): Promise<void> {
        try {
            const cliPath = await this.downloadCli();
            vscode.window.showInformationMessage(`GPU Go CLI downloaded to: ${cliPath}`);
        } catch (error) {
            const errorMessage = error instanceof Error ? error.message : String(error);
            vscode.window.showErrorMessage(`Failed to download CLI: ${errorMessage}`);
        }
    }
}
