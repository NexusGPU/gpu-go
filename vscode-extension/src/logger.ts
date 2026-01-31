import * as vscode from 'vscode';

/**
 * Centralized logging utility for the GPU Go extension.
 * Outputs to VS Code's Output panel with timestamps.
 */
export class Logger {
    private static outputChannel: vscode.OutputChannel | null = null;
    private static channelName: string = 'GPU Go';

    /**
     * Initialize the logger with a VS Code extension context.
     * Must be called during extension activation.
     */
    public static initialize(context: vscode.ExtensionContext, name: string = 'GPU Go'): void {
        this.channelName = name;
        this.outputChannel = vscode.window.createOutputChannel(name);
        context.subscriptions.push(this.outputChannel);
    }

    /**
     * Get the current timestamp in ISO format.
     */
    private static getTimestamp(): string {
        return new Date().toISOString();
    }

    /**
     * Format a message with timestamp and level.
     */
    private static formatMessage(level: string, message: string): string {
        return `[${this.getTimestamp()}] [${level}] ${message}`;
    }

    /**
     * Log an informational message.
     */
    public static log(message: string): void {
        const formatted = this.formatMessage('INFO', message);
        if (this.outputChannel) {
            this.outputChannel.appendLine(formatted);
        } else {
            console.log(formatted);
        }
    }

    /**
     * Log a warning message.
     */
    public static warn(message: string): void {
        const formatted = this.formatMessage('WARN', message);
        if (this.outputChannel) {
            this.outputChannel.appendLine(formatted);
        } else {
            console.warn(formatted);
        }
    }

    /**
     * Log an error message with optional error details.
     */
    public static error(message: string, error?: unknown): void {
        const formatted = this.formatMessage('ERROR', message);
        if (this.outputChannel) {
            this.outputChannel.appendLine(formatted);
            if (error) {
                if (error instanceof Error) {
                    this.outputChannel.appendLine(`  ${error.message}`);
                    if (error.stack) {
                        this.outputChannel.appendLine(`  Stack: ${error.stack}`);
                    }
                } else if (typeof error === 'string') {
                    this.outputChannel.appendLine(`  ${error}`);
                } else {
                    this.outputChannel.appendLine(`  ${JSON.stringify(error, null, 2)}`);
                }
            }
        } else {
            console.error(formatted, error);
        }
    }

    /**
     * Log a debug message (only when debug mode is enabled).
     */
    public static debug(message: string): void {
        const config = vscode.workspace.getConfiguration('gpugo');
        const debugEnabled = config.get<boolean>('debug', false);

        if (!debugEnabled) {
            return;
        }

        const formatted = this.formatMessage('DEBUG', message);
        if (this.outputChannel) {
            this.outputChannel.appendLine(formatted);
        } else {
            console.debug(formatted);
        }
    }

    /**
     * Show the output channel in the VS Code panel.
     */
    public static show(): void {
        this.outputChannel?.show();
    }

    /**
     * Clear the output channel.
     */
    public static clear(): void {
        this.outputChannel?.clear();
    }
}
