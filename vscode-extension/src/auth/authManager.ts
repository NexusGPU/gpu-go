import * as vscode from 'vscode';
import * as fs from 'fs/promises';
import * as path from 'path';
import * as os from 'os';
import { CLI } from '../cli/cli';

const LOGIN_URL = 'https://go.tensor-fusion.ai/settings/security#ide-extension';

export class AuthManager {
    private context: vscode.ExtensionContext;
    private cli: CLI;
    private _isLoggedIn: boolean = false;
    private _onAuthStateChanged: vscode.EventEmitter<boolean> = new vscode.EventEmitter<boolean>();
    readonly onAuthStateChanged: vscode.Event<boolean> = this._onAuthStateChanged.event;

    constructor(context: vscode.ExtensionContext, cli: CLI) {
        this.context = context;
        this.cli = cli;
    }

    get isLoggedIn(): boolean {
        return this._isLoggedIn;
    }

    async checkLoginStatus(): Promise<boolean> {
        try {
            const tokenPath = this.getTokenPath();
            await fs.access(tokenPath);
            
            // Read and validate token
            const content = await fs.readFile(tokenPath, 'utf-8');
            const tokenConfig = JSON.parse(content);
            
            if (tokenConfig.token) {
                // Check if expired
                if (tokenConfig.expires_at) {
                    const expiresAt = new Date(tokenConfig.expires_at);
                    if (expiresAt < new Date()) {
                        this._isLoggedIn = false;
                        this._onAuthStateChanged.fire(false);
                        return false;
                    }
                }
                
                this._isLoggedIn = true;
                this._onAuthStateChanged.fire(true);
                return true;
            }
        } catch {
            // Token file doesn't exist or is invalid
        }
        
        this._isLoggedIn = false;
        this._onAuthStateChanged.fire(false);
        return false;
    }

    async login(): Promise<boolean> {
        // First, open browser to generate PAT
        const openBrowser = await vscode.window.showInformationMessage(
            'To login, you need to generate a Personal Access Token (PAT) from the GPU Go dashboard.',
            'Open Dashboard',
            'I have a token'
        );

        if (openBrowser === 'Open Dashboard') {
            await vscode.env.openExternal(vscode.Uri.parse(LOGIN_URL));
            
            // Show message to wait for token
            await vscode.window.showInformationMessage(
                'After generating your PAT, click "Enter Token" to continue.',
                'Enter Token'
            );
        }

        // Prompt for token input
        const token = await vscode.window.showInputBox({
            prompt: 'Enter your Personal Access Token (PAT)',
            password: true,
            placeHolder: 'Paste your PAT here...',
            validateInput: (value) => {
                if (!value || value.trim().length === 0) {
                    return 'Token cannot be empty';
                }
                if (value.length < 20) {
                    return 'Token seems too short. Please check and try again.';
                }
                return null;
            }
        });

        if (!token) {
            return false;
        }

        try {
            // Save token using CLI or directly to file
            await this.saveToken(token.trim());
            
            this._isLoggedIn = true;
            this._onAuthStateChanged.fire(true);
            
            vscode.window.showInformationMessage('Successfully logged in to GPU Go!');
            return true;
        } catch (error) {
            vscode.window.showErrorMessage(`Login failed: ${error}`);
            return false;
        }
    }

    async logout(): Promise<void> {
        const confirm = await vscode.window.showWarningMessage(
            'Are you sure you want to logout from GPU Go?',
            { modal: true },
            'Logout'
        );

        if (confirm !== 'Logout') {
            return;
        }

        try {
            const tokenPath = this.getTokenPath();
            await fs.unlink(tokenPath);
            
            this._isLoggedIn = false;
            this._onAuthStateChanged.fire(false);
            
            vscode.window.showInformationMessage('Successfully logged out from GPU Go.');
        } catch (error) {
            vscode.window.showErrorMessage(`Logout failed: ${error}`);
        }
    }

    async getToken(): Promise<string | null> {
        try {
            const tokenPath = this.getTokenPath();
            const content = await fs.readFile(tokenPath, 'utf-8');
            const tokenConfig = JSON.parse(content);
            return tokenConfig.token || null;
        } catch {
            return null;
        }
    }

    private async saveToken(token: string): Promise<void> {
        const tokenPath = this.getTokenPath();
        const tokenDir = path.dirname(tokenPath);

        // Ensure directory exists
        await fs.mkdir(tokenDir, { recursive: true, mode: 0o700 });

        const tokenConfig = {
            token: token,
            created_at: new Date().toISOString(),
            expires_at: new Date(Date.now() + 365 * 24 * 60 * 60 * 1000).toISOString() // 1 year
        };

        await fs.writeFile(tokenPath, JSON.stringify(tokenConfig, null, 2), {
            mode: 0o600
        });
    }

    private getTokenPath(): string {
        const homeDir = os.homedir();
        return path.join(homeDir, '.gpugo', 'token.json');
    }

    // Handle 403 errors by prompting for re-login
    async handleAuthError(): Promise<boolean> {
        const action = await vscode.window.showErrorMessage(
            'Authentication failed. Your session may have expired.',
            'Login Again',
            'Cancel'
        );

        if (action === 'Login Again') {
            return this.login();
        }

        return false;
    }
}
