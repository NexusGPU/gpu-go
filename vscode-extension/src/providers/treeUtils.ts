import * as vscode from 'vscode';

/**
 * Base class for tree data providers with common functionality
 */
export abstract class BaseTreeDataProvider<T> implements vscode.TreeDataProvider<T> {
    protected _onDidChangeTreeData = new vscode.EventEmitter<T | undefined | null | void>();
    readonly onDidChangeTreeData = this._onDidChangeTreeData.event;

    refresh(): void {
        this._onDidChangeTreeData.fire();
    }

    abstract getTreeItem(element: T): vscode.TreeItem;
    abstract getChildren(element?: T): Promise<T[]>;
}

/**
 * Creates a standard login prompt tree item
 */
export function createLoginItem(): vscode.TreeItem {
    const item = new vscode.TreeItem('Login to GPUGo', vscode.TreeItemCollapsibleState.None);
    item.iconPath = new vscode.ThemeIcon('account');
    item.command = {
        command: 'gpugo.login',
        title: 'Login'
    };
    return item;
}

/**
 * Creates a standard empty state tree item
 */
export function createEmptyItem(message: string, hint?: string): vscode.TreeItem {
    const item = new vscode.TreeItem(message, vscode.TreeItemCollapsibleState.None);
    item.iconPath = new vscode.ThemeIcon('info');
    if (hint) {
        item.description = hint;
    }
    return item;
}

/**
 * Creates a standard error tree item
 */
export function createErrorItem(prefix: string, error: unknown): vscode.TreeItem {
    const item = new vscode.TreeItem(prefix, vscode.TreeItemCollapsibleState.None);
    item.iconPath = new vscode.ThemeIcon('error');
    item.tooltip = String(error);
    return item;
}

/**
 * A unified property item for displaying key-value pairs in tree views
 */
export class PropertyItem extends vscode.TreeItem {
    constructor(
        label: string, 
        value: string, 
        options?: { 
            icon?: string | vscode.ThemeIcon; 
            command?: vscode.Command;
            description?: string;
        }
    ) {
        super(`${label}: ${value}`, vscode.TreeItemCollapsibleState.None);
        this.description = options?.description ?? '';
        
        if (options?.icon) {
            this.iconPath = typeof options.icon === 'string' 
                ? new vscode.ThemeIcon(options.icon)
                : options.icon;
        }
        if (options?.command) {
            this.command = options.command;
        }
    }
}

/**
 * Creates an action item (like "Add GPU Server")
 */
export function createActionItem(
    label: string, 
    command: string, 
    icon: string = 'add',
    tooltip?: string
): vscode.TreeItem {
    const item = new vscode.TreeItem(label, vscode.TreeItemCollapsibleState.None);
    item.iconPath = new vscode.ThemeIcon(icon);
    item.command = {
        command,
        title: label
    };
    if (tooltip) {
        item.tooltip = tooltip;
    }
    return item;
}

/**
 * Status utilities for tree items
 */
export const StatusIcons = {
    running: (color = 'charts.green') => new vscode.ThemeIcon('vm-running', new vscode.ThemeColor(color)),
    stopped: () => new vscode.ThemeIcon('vm-outline'),
    pending: () => new vscode.ThemeIcon('loading~spin'),
    online: (icon = 'server', color = 'charts.green') => new vscode.ThemeIcon(icon, new vscode.ThemeColor(color)),
    offline: (icon = 'server', color = 'charts.red') => new vscode.ThemeIcon(icon, new vscode.ThemeColor(color)),
    error: () => new vscode.ThemeIcon('error', new vscode.ThemeColor('charts.red')),
} as const;

/**
 * Gets appropriate icon for a status string
 */
export function getStatusIcon(status: string, entityType: 'studio' | 'worker' | 'agent' = 'worker'): vscode.ThemeIcon {
    const statusLower = status.toLowerCase();
    
    if (statusLower === 'running' || statusLower === 'online') {
        if (entityType === 'studio') {
            return StatusIcons.running();
        }
        if (entityType === 'agent') {
            return StatusIcons.online('server');
        }
        return new vscode.ThemeIcon('broadcast', new vscode.ThemeColor('charts.green'));
    }
    
    if (statusLower === 'stopped' || statusLower === 'exited' || statusLower === 'offline') {
        if (entityType === 'studio') {
            return StatusIcons.stopped();
        }
        if (entityType === 'agent') {
            return StatusIcons.offline('server');
        }
        return new vscode.ThemeIcon('broadcast', new vscode.ThemeColor('charts.red'));
    }
    
    return StatusIcons.pending();
}

/**
 * Gets context value for a status
 */
export function getStatusContext(status: string, prefix: string): string {
    const statusLower = status.toLowerCase();
    if (statusLower === 'running' || statusLower === 'online') {
        return `${prefix}-running`;
    }
    if (statusLower === 'stopped' || statusLower === 'exited' || statusLower === 'offline') {
        return `${prefix}-stopped`;
    }
    return `${prefix}-pending`;
}
