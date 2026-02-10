import * as vscode from 'vscode';
import { CLI } from '../cli/cli';
import { getWebviewContent } from './webviewUtils';
import { STUDIO_TEMPLATES, getTemplateById, getCategories } from '../config/studioTemplates';

export class CreateStudioPanel {
    public static currentPanel: CreateStudioPanel | undefined;
    public static readonly viewType = 'gpugo.createStudio';

    private readonly _panel: vscode.WebviewPanel;
    private readonly _extensionUri: vscode.Uri;
    private readonly _cli: CLI;
    private _disposables: vscode.Disposable[] = [];

    public static createOrShow(extensionUri: vscode.Uri, cli: CLI) {
        const column = vscode.window.activeTextEditor
            ? vscode.window.activeTextEditor.viewColumn
            : undefined;

        // If panel exists, show it
        if (CreateStudioPanel.currentPanel) {
            CreateStudioPanel.currentPanel._panel.reveal(column);
            return;
        }

        // Create new panel
        const panel = vscode.window.createWebviewPanel(
            CreateStudioPanel.viewType,
            'Create Studio Environment',
            column || vscode.ViewColumn.One,
            {
                enableScripts: true
            }
        );

        CreateStudioPanel.currentPanel = new CreateStudioPanel(panel, extensionUri, cli);
    }

    private constructor(panel: vscode.WebviewPanel, extensionUri: vscode.Uri, cli: CLI) {
        this._panel = panel;
        this._extensionUri = extensionUri;
        this._cli = cli;

        // Set content
        this.updateContent();

        // Handle disposal
        this._panel.onDidDispose(() => this.dispose(), null, this._disposables);

        // Handle messages
        this._panel.webview.onDidReceiveMessage(
            async message => {
                switch (message.command) {
                    case 'create':
                        await this.createStudio(message.data);
                        break;
                    case 'fetchTags':
                        await this.handleFetchTags(message.image, message.registry);
                        break;
                }
            },
            null,
            this._disposables
        );
    }

    private async updateContent() {
        // Get all backends with status
        const allBackends = await this._cli.studioBackendsAll();
        const defaultRegistry = vscode.workspace.getConfiguration('gpugo').get<string>('defaultRegistry', 'docker.io');

        this._panel.webview.html = this.getHtmlForWebview(allBackends, defaultRegistry);
    }

    private async createStudio(data: {
        name: string;
        mode: string;
        template: string;
        customImage: string;
        gpuUrl: string;
        ports: string;
        volumes: string;
        envs: string;
        registry: string;
        versionTag: string;
    }) {
        try {
            // Resolve image from template or custom
            let image = data.customImage;
            let ports = data.ports ? data.ports.split(',').map(p => p.trim()).filter(Boolean) : [];
            let envs: string[] = [];

            if (data.template && data.template !== 'custom') {
                const template = getTemplateById(data.template);
                if (template) {
                    // Parse template image: "namespace/repo:tag"
                    const [imageWithoutTag, defaultTag] = template.image.split(':');
                    const tag = data.versionTag || defaultTag || 'latest';
                    const registry = data.registry && data.registry !== 'docker.io' ? data.registry : '';

                    // Compose final image
                    image = registry
                        ? `${registry}/${imageWithoutTag}:${tag}`
                        : `${imageWithoutTag}:${tag}`;

                    // Use default ports if not specified
                    if (ports.length === 0 && template.defaultPorts) {
                        ports = template.defaultPorts;
                    }
                    // Add default environment variables
                    if (template.defaultEnv) {
                        envs = Object.entries(template.defaultEnv).map(([k, v]) => `${k}=${v}`);
                    }
                }
            }

            // Add user-specified env vars
            if (data.envs) {
                const userEnvs = data.envs.split(',').map(e => e.trim()).filter(Boolean);
                envs.push(...userEnvs);
            }

            const volumes = data.volumes ? data.volumes.split(',').map(v => v.trim()).filter(Boolean) : [];

            await vscode.window.withProgress({
                location: vscode.ProgressLocation.Notification,
                title: `Creating studio '${data.name}'...`,
                cancellable: false
            }, async () => {
                await this._cli.studioCreate(data.name, {
                    mode: data.mode || undefined,
                    image: image || undefined,
                    gpuUrl: data.gpuUrl || undefined,
                    ports: ports.length > 0 ? ports : undefined,
                    volumes: volumes.length > 0 ? volumes : undefined,
                    envs: envs.length > 0 ? envs : undefined
                });
            });

            vscode.commands.executeCommand('gpugo.refreshStudio');
            this._panel.dispose();

            // Show toast with Connect via SSH action
            const action = await vscode.window.showInformationMessage(
                `Studio '${data.name}' created!`,
                'Connect via SSH'
            );
            if (action === 'Connect via SSH') {
                vscode.commands.executeCommand('opensshremotes.openEmptyWindow', { host: `ggo-${data.name}` });
            }
        } catch (error) {
            vscode.window.showErrorMessage(`Failed to create studio: ${error}`);
        }
    }

    private async handleFetchTags(image: string, registry?: string) {
        try {
            const tags = await this._cli.studioTags(image, registry);
            this._panel.webview.postMessage({ command: 'tagsResult', tags });
        } catch {
            this._panel.webview.postMessage({ command: 'tagsResult', tags: [] });
        }
    }

    private getHtmlForWebview(allBackends: { name: string; mode: string; available: boolean; installed: boolean }[], defaultRegistry: string): string {
        const webview = this._panel.webview;
        const nonce = getNonce();

        // Backend options with status indicators
        const backendDisplayNames: Record<string, string> = {
            'auto': 'Auto-detect',
            'docker': 'Docker',
            'colima': 'Colima',
            'wsl': 'WSL (Windows)',
            'apple-container': 'Apple Container (macOS 26+)',
            'podman': 'Podman',
            'lima': 'Lima'
        };

        // Build backend options: auto first, then available, then others with status
        const backendOptions: string[] = ['<vscode-option value="auto">Auto-detect</vscode-option>'];

        // Sort: available first, then installed but not running, then not installed
        const sorted = [...allBackends].sort((a, b) => {
            if (a.available !== b.available) {return a.available ? -1 : 1;}
            if (a.installed !== b.installed) {return a.installed ? -1 : 1;}
            return a.name.localeCompare(b.name);
        });

        for (const b of sorted) {
            const displayName = backendDisplayNames[b.mode] || b.name;
            let statusHint = '';
            if (!b.available && b.installed) {
                statusHint = ' (not running)';
            } else if (!b.available && !b.installed) {
                statusHint = ' (not installed)';
            }
            backendOptions.push(
                `<vscode-option value="${b.mode}"${!b.available ? ' disabled' : ''}>${displayName}${statusHint}</vscode-option>`
            );
        }

        // Generate template options grouped by category with cleaner display
        const categories = getCategories();
        const templateOptionsHtml = categories.map(category => {
            const templates = STUDIO_TEMPLATES.filter(t => t.category === category.id);
            const options = templates.map(t => {
                // Shorter option text - just icon and name for dropdown
                const levelBadge = t.level === 'beginner' ? '⭐' : t.level === 'advanced' ? '🔧' : '';
                return `<vscode-option value="${t.id}">${t.icon} ${t.name}${levelBadge ? ' ' + levelBadge : ''}</vscode-option>`;
            }).join('');
            return options;
        }).join('');

        // Generate template info as JSON for JavaScript
        const templatesJson = JSON.stringify(STUDIO_TEMPLATES);

        const content = `
            <div class="header">
                <h1><vscode-icon name="vm"></vscode-icon> Create Studio Environment</h1>
            </div>

            <p class="description">
                Create a new AI development studio environment with remote GPU access.
                The environment will be configured with SSH for VS Code Remote connection.
            </p>

            <vscode-divider></vscode-divider>

            <vscode-form-container id="create-form">
                <vscode-form-group variant="vertical">
                    <vscode-label for="name">Environment Name *</vscode-label>
                    <vscode-textfield id="name" name="name" placeholder="my-studio" required></vscode-textfield>
                    <vscode-form-helper>A unique name for your studio environment</vscode-form-helper>
                </vscode-form-group>

                <vscode-form-group variant="vertical">
                    <vscode-label for="mode">Backend Mode</vscode-label>
                    <vscode-single-select id="mode" name="mode">
                        ${backendOptions.join('')}
                    </vscode-single-select>
                    <vscode-form-helper>Container runtime for running the studio environment</vscode-form-helper>
                </vscode-form-group>

                <vscode-form-group variant="vertical">
                    <vscode-label for="template">Image Template</vscode-label>
                    <vscode-single-select id="template" name="template">
                        ${templateOptionsHtml}
                    </vscode-single-select>
                    <vscode-form-helper id="template-helper">Pre-configured AI/ML development image</vscode-form-helper>
                </vscode-form-group>

                <vscode-form-group variant="vertical" id="custom-image-group" style="display: none;">
                    <vscode-label for="customImage">Custom Image</vscode-label>
                    <vscode-textfield id="customImage" name="customImage" placeholder="e.g., pytorch/pytorch:2.0-cuda11.8"></vscode-textfield>
                    <vscode-form-helper>Enter your custom Docker image</vscode-form-helper>
                </vscode-form-group>

                <div id="template-info" class="template-info-box" style="margin: 16px 0; padding: 16px; background: var(--vscode-textBlockQuote-background); border-radius: 6px; border-left: 4px solid var(--vscode-textLink-foreground); display: none;">
                    <div style="display: flex; align-items: center; gap: 8px; margin-bottom: 12px;">
                        <span id="info-icon" style="font-size: 1.5em;"></span>
                        <div>
                            <div id="info-name" style="font-weight: 600; font-size: 1.1em;"></div>
                            <div id="info-description" style="color: var(--vscode-descriptionForeground); font-size: 0.9em;"></div>
                        </div>
                        <vscode-badge id="info-level" style="margin-left: auto;"></vscode-badge>
                    </div>
                    
                    <div style="display: grid; grid-template-columns: auto 1fr; gap: 8px 16px; font-size: 0.9em;">
                        <span style="color: var(--vscode-descriptionForeground);">🐳 Image:</span>
                        <code id="info-image" style="font-family: var(--vscode-editor-font-family); background: var(--vscode-textCodeBlock-background); padding: 2px 6px; border-radius: 4px;"></code>
                        
                        <span id="info-features-label" style="color: var(--vscode-descriptionForeground);">📦 Features:</span>
                        <span id="info-features"></span>
                        
                        <span id="info-ports-label" style="color: var(--vscode-descriptionForeground); display: none;">🔌 Ports:</span>
                        <span id="info-ports" style="display: none;"></span>
                        
                        <span id="info-urls-label" style="color: var(--vscode-descriptionForeground); display: none;">🌐 Web Access:</span>
                        <span id="info-urls" style="display: none;"></span>
                    </div>
                </div>

                <div id="image-options-group" style="display: none;">
                    <vscode-form-group variant="vertical">
                        <vscode-label for="registry">Image Registry</vscode-label>
                        <vscode-textfield id="registry" name="registry" value="${defaultRegistry}" placeholder="docker.io"></vscode-textfield>
                        <vscode-form-helper>Container registry (default: docker.io)</vscode-form-helper>
                    </vscode-form-group>

                    <vscode-form-group variant="vertical" style="margin-top: var(--spacing-md);">
                        <vscode-label for="versionTag">Image Version</vscode-label>
                        <input id="versionTag" list="version-tags" value="latest" placeholder="latest"
                            style="width: 100%; padding: 6px 8px; background: var(--vscode-input-background); color: var(--vscode-input-foreground); border: 1px solid var(--vscode-input-border, transparent); border-radius: 2px; font-family: var(--vscode-font-family); font-size: var(--vscode-font-size);" />
                        <datalist id="version-tags"></datalist>
                        <vscode-form-helper>Image tag/version (type to search available tags)</vscode-form-helper>
                    </vscode-form-group>
                </div>

                <vscode-form-group variant="vertical">
                    <vscode-label for="gpuUrl">Remote GPU Share Link</vscode-label>
                    <vscode-textfield id="gpuUrl" name="gpuUrl" placeholder="e.g., abc123 or https://go.gpu.tf/s/abc123"></vscode-textfield>
                    <vscode-form-helper>Share link or code to a remote vGPU worker. If left empty, will use local GPU if applicable.</vscode-form-helper>
                </vscode-form-group>

                <vscode-collapsible title="Advanced Options">
                    <vscode-form-group variant="vertical">
                        <vscode-label for="ports">Port Mappings</vscode-label>
                        <vscode-textfield id="ports" name="ports" placeholder="8888:8888, 6006:6006"></vscode-textfield>
                        <vscode-form-helper>Comma-separated port mappings (host:container). Leave empty to use template defaults.</vscode-form-helper>
                    </vscode-form-group>

                    <vscode-form-group variant="vertical">
                        <vscode-label for="volumes">Volume Mounts</vscode-label>
                        <vscode-textfield id="volumes" name="volumes" placeholder="~/projects:/workspace"></vscode-textfield>
                        <vscode-form-helper>Comma-separated volume mounts (host:container)</vscode-form-helper>
                    </vscode-form-group>

                    <vscode-form-group variant="vertical">
                        <vscode-label for="envs">Environment Variables</vscode-label>
                        <vscode-textfield id="envs" name="envs" placeholder="KEY1=value1, KEY2=value2"></vscode-textfield>
                        <vscode-form-helper>Comma-separated environment variables (KEY=VALUE)</vscode-form-helper>
                    </vscode-form-group>
                </vscode-collapsible>

                <vscode-divider></vscode-divider>

                <div class="actions">
                    <vscode-button id="create-btn">
                        <vscode-icon name="add" slot="start"></vscode-icon>
                        Create Studio
                    </vscode-button>
                </div>
            </vscode-form-container>

            <script nonce="${nonce}">
                const vscode = acquireVsCodeApi();
                const templates = ${templatesJson};

                // Request tags from extension host via CLI
                function fetchTags(image, registry) {
                    vscode.postMessage({ command: 'fetchTags', image: image, registry: registry || '' });
                }

                // Handle messages from extension host
                window.addEventListener('message', event => {
                    const message = event.data;
                    if (message.command === 'tagsResult') {
                        const datalist = document.getElementById('version-tags');
                        datalist.textContent = '';
                        (message.tags || []).forEach(tag => {
                            const opt = document.createElement('option');
                            opt.value = tag;
                            datalist.appendChild(opt);
                        });
                    }
                });

                // Update UI when template changes
                document.getElementById('template').addEventListener('change', (e) => {
                    const templateId = e.target.value;
                    const template = templates.find(t => t.id === templateId);
                    const imageOptionsGroup = document.getElementById('image-options-group');

                    if (template) {
                        // Show/hide custom image field
                        const customImageGroup = document.getElementById('custom-image-group');
                        if (template.id === 'custom') {
                            customImageGroup.style.display = 'block';
                            imageOptionsGroup.style.display = 'none';
                        } else {
                            customImageGroup.style.display = 'none';
                            imageOptionsGroup.style.display = 'block';
                            // Fetch tags for the template image via CLI
                            const registry = document.getElementById('registry').value;
                            fetchTags(template.image, registry);
                        }

                        // Show template info
                        const infoBox = document.getElementById('template-info');
                        if (template.id !== 'custom') {
                            infoBox.style.display = 'block';

                            // Basic info
                            document.getElementById('info-icon').textContent = template.icon;
                            document.getElementById('info-name').textContent = template.name;
                            document.getElementById('info-description').textContent = template.description;
                            document.getElementById('info-image').textContent = template.image;

                            // Level badge
                            const levelBadge = document.getElementById('info-level');
                            const levelLabels = {
                                'beginner': '⭐ Beginner',
                                'intermediate': '📚 Intermediate',
                                'advanced': '🔧 Advanced'
                            };
                            levelBadge.textContent = levelLabels[template.level] || '';
                            levelBadge.style.display = template.level ? 'inline-block' : 'none';

                            // Features
                            document.getElementById('info-features').textContent = template.features.join(', ') || 'N/A';

                            // Ports
                            const portsLabel = document.getElementById('info-ports-label');
                            const portsEl = document.getElementById('info-ports');
                            if (template.defaultPorts && template.defaultPorts.length > 0) {
                                portsLabel.style.display = 'block';
                                portsEl.style.display = 'block';
                                portsEl.textContent = template.defaultPorts.join(', ');
                            } else {
                                portsLabel.style.display = 'none';
                                portsEl.style.display = 'none';
                            }

                            // Web URLs - uses safe DOM construction
                            const urlsLabel = document.getElementById('info-urls-label');
                            const urlsEl = document.getElementById('info-urls');
                            if (template.webUrls && template.webUrls.length > 0) {
                                urlsLabel.style.display = 'block';
                                urlsEl.style.display = 'block';
                                urlsEl.textContent = '';
                                template.webUrls.forEach(u => {
                                    const span = document.createElement('span');
                                    span.style.cssText = 'background: var(--vscode-badge-background); color: var(--vscode-badge-foreground); padding: 2px 8px; border-radius: 4px; margin-right: 6px; font-size: 0.85em;';
                                    span.textContent = u.name + ' :' + u.port;
                                    urlsEl.appendChild(span);
                                });
                            } else {
                                urlsLabel.style.display = 'none';
                                urlsEl.style.display = 'none';
                            }

                            // Auto-fill ports if not specified
                            const portsField = document.getElementById('ports');
                            if (!portsField.value && template.defaultPorts) {
                                portsField.value = template.defaultPorts.join(', ');
                            }
                        } else {
                            infoBox.style.display = 'none';
                        }
                    }
                });

                // Trigger initial template info display
                const initialTemplate = document.getElementById('template').value;
                if (initialTemplate) {
                    document.getElementById('template').dispatchEvent(new Event('change'));
                }

                document.getElementById('create-btn').addEventListener('click', () => {
                    const name = document.getElementById('name').value;
                    if (!name) {
                        alert('Please enter a name for the studio environment');
                        return;
                    }

                    const templateId = document.getElementById('template').value;
                    const customImage = document.getElementById('customImage').value;

                    if (templateId === 'custom' && !customImage) {
                        alert('Please enter a custom image name');
                        return;
                    }

                    const data = {
                        name: name,
                        mode: document.getElementById('mode').value,
                        template: templateId,
                        customImage: customImage,
                        gpuUrl: document.getElementById('gpuUrl').value,
                        ports: document.getElementById('ports').value,
                        volumes: document.getElementById('volumes').value,
                        envs: document.getElementById('envs').value,
                        registry: document.getElementById('registry').value,
                        versionTag: document.getElementById('versionTag').value
                    };

                    vscode.postMessage({ command: 'create', data: data });
                });
            </script>
        `;

        return getWebviewContent(webview, this._extensionUri, nonce, content);
    }

    public dispose() {
        CreateStudioPanel.currentPanel = undefined;
        this._panel.dispose();
        while (this._disposables.length) {
            const d = this._disposables.pop();
            if (d) {
                d.dispose();
            }
        }
    }
}

function getNonce() {
    let text = '';
    const possible = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789';
    for (let i = 0; i < 32; i++) {
        text += possible.charAt(Math.floor(Math.random() * possible.length));
    }
    return text;
}
