import * as vscode from 'vscode';

export function getWebviewContent(
    webview: vscode.Webview,
    extensionUri: vscode.Uri,
    nonce: string,
    content: string
): string {
    // Using CDN for all resources
    const elementsScript = 'https://cdn.jsdelivr.net/npm/@vscode-elements/elements@1/dist/bundled.js';
    const codiconsCSS = 'https://cdn.jsdelivr.net/npm/@vscode/codicons@0.0.35/dist/codicon.css';

    return `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <meta http-equiv="Content-Security-Policy" content="default-src 'none'; style-src ${webview.cspSource} 'unsafe-inline' https://cdn.jsdelivr.net; font-src ${webview.cspSource} https://cdn.jsdelivr.net; script-src 'nonce-${nonce}' https://cdn.jsdelivr.net; img-src ${webview.cspSource} https: data:;">
    <link rel="stylesheet" href="${codiconsCSS}">
    <script type="module" src="${elementsScript}" nonce="${nonce}"></script>
    <title>GPUGo</title>
    <style>
        :root {
            --spacing-xs: 4px;
            --spacing-sm: 8px;
            --spacing-md: 16px;
            --spacing-lg: 24px;
            --spacing-xl: 32px;
        }

        body {
            padding: var(--spacing-lg) var(--spacing-xl);
            color: var(--vscode-foreground);
            font-family: var(--vscode-font-family);
            font-size: var(--vscode-font-size);
            line-height: 1.5;
            max-width: 900px;
            margin: 0 auto;
        }

        h1 {
            display: flex;
            align-items: center;
            gap: var(--spacing-sm);
            margin: 0 0 var(--spacing-md) 0;
            font-size: 1.5em;
            font-weight: 600;
        }

        h2 {
            display: flex;
            align-items: center;
            gap: var(--spacing-sm);
            margin: var(--spacing-lg) 0 var(--spacing-md) 0;
            font-size: 1.2em;
            font-weight: 600;
        }

        h3 {
            margin: var(--spacing-md) 0 var(--spacing-sm) 0;
            font-size: 1.1em;
            font-weight: 600;
        }

        p {
            margin: 0 0 var(--spacing-md) 0;
        }

        .description {
            color: var(--vscode-descriptionForeground);
        }

        .header {
            display: flex;
            align-items: center;
            justify-content: space-between;
            gap: var(--spacing-md);
        }

        .actions {
            display: flex;
            gap: var(--spacing-sm);
            margin-top: var(--spacing-lg);
        }

        .info-box {
            display: flex;
            gap: var(--spacing-md);
            padding: var(--spacing-md);
            background: var(--vscode-textBlockQuote-background);
            border-left: 3px solid var(--vscode-textLink-foreground);
            margin: var(--spacing-lg) 0;
        }

        .info-box vscode-icon {
            flex-shrink: 0;
            color: var(--vscode-textLink-foreground);
        }

        .info-box p {
            margin: 0;
        }

        .info-box ul, .info-box ol {
            margin: var(--spacing-sm) 0 0 0;
            padding-left: var(--spacing-lg);
        }

        .info-box li {
            margin: var(--spacing-xs) 0;
        }

        .code-block {
            display: flex;
            align-items: center;
            gap: var(--spacing-sm);
            padding: var(--spacing-md);
            background: var(--vscode-textCodeBlock-background);
            border-radius: 4px;
            font-family: var(--vscode-editor-font-family);
            font-size: var(--vscode-editor-font-size);
            overflow-x: auto;
        }

        .code-block code {
            flex: 1;
            white-space: nowrap;
        }

        .options-container {
            display: flex;
            flex-direction: column;
            gap: var(--spacing-md);
        }

        .option-content {
            padding: var(--spacing-md);
        }

        ul {
            margin: var(--spacing-sm) 0;
            padding-left: var(--spacing-lg);
        }

        li {
            margin: var(--spacing-xs) 0;
        }

        vscode-divider {
            margin: var(--spacing-lg) 0;
        }

        vscode-form-container {
            display: flex;
            flex-direction: column;
            gap: var(--spacing-md);
        }

        vscode-form-group {
            display: flex;
            flex-direction: column;
            gap: var(--spacing-xs);
        }

        vscode-table {
            margin: var(--spacing-md) 0;
        }

        vscode-collapsible {
            margin: var(--spacing-sm) 0;
        }

        vscode-badge {
            padding: 2px 8px;
            border-radius: 4px;
        }

        /* Detail grid layout */
        .detail-grid {
            display: grid;
            grid-template-columns: repeat(auto-fill, minmax(200px, 1fr));
            gap: var(--spacing-md);
            margin: var(--spacing-md) 0;
            background: var(--vscode-textBlockQuote-background);
            border-radius: 8px;
            padding: var(--spacing-md) var(--spacing-lg);
        }

        .detail-item {
            display: flex;
            flex-direction: column;
            gap: var(--spacing-xs);
        }

        .detail-label {
            font-size: 0.85em;
            color: var(--vscode-descriptionForeground);
            text-transform: uppercase;
            letter-spacing: 0.05em;
        }

        .detail-value {
            font-family: var(--vscode-editor-font-family);
            font-size: var(--vscode-editor-font-size);
            word-break: break-all;
        }

        /* GPU cards */
        .gpu-cards {
            display: grid;
            grid-template-columns: repeat(auto-fill, minmax(280px, 1fr));
            gap: var(--spacing-md);
            margin: var(--spacing-md) 0;
        }

        .gpu-card {
            border: 1px solid var(--vscode-widget-border, var(--vscode-editorWidget-border, rgba(128,128,128,0.35)));
            border-radius: 8px;
            overflow: hidden;
            transition: border-color 0.15s ease;
        }

        .gpu-card:hover {
            border-color: var(--vscode-focusBorder);
        }

        .gpu-card-header {
            display: flex;
            align-items: center;
            gap: var(--spacing-sm);
            padding: var(--spacing-sm) var(--spacing-md);
            background: var(--vscode-textBlockQuote-background);
            border-bottom: 1px solid var(--vscode-widget-border, var(--vscode-editorWidget-border, rgba(128,128,128,0.35)));
        }

        .gpu-card-icon {
            color: var(--vscode-charts-yellow);
        }

        .gpu-card-title {
            flex: 1;
            font-weight: 600;
            font-size: 0.95em;
        }

        .gpu-card-body {
            padding: var(--spacing-md);
            display: flex;
            flex-direction: column;
            gap: var(--spacing-sm);
        }

        .gpu-card-stat {
            display: flex;
            justify-content: space-between;
            align-items: center;
        }

        .gpu-stat-label {
            font-size: 0.85em;
            color: var(--vscode-descriptionForeground);
        }

        .gpu-stat-value {
            font-family: var(--vscode-editor-font-family);
            font-size: var(--vscode-editor-font-size);
        }

        .gpu-id-value {
            font-size: 0.85em;
            opacity: 0.8;
        }

        /* Share links */
        .share-links {
            display: flex;
            flex-direction: column;
            gap: var(--spacing-sm);
            margin: var(--spacing-md) 0;
        }

        .share-link-item {
            display: flex;
            align-items: center;
            gap: var(--spacing-sm);
            padding: var(--spacing-sm) var(--spacing-md);
            background: var(--vscode-textCodeBlock-background);
            border-radius: 4px;
        }

        .share-link-item code {
            flex: 1;
            font-family: var(--vscode-editor-font-family);
        }

        .share-link-item .copy-share-btn,
        .share-link-item vscode-button[appearance="icon"] {
            color: var(--vscode-foreground);
        }

        /* Responsive adjustments */
        @media (max-width: 600px) {
            body {
                padding: var(--spacing-md);
                max-width: 100%;
            }

            .header {
                flex-direction: column;
                align-items: flex-start;
            }

            .actions {
                flex-direction: column;
            }

            .actions vscode-button {
                width: 100%;
            }
        }
    </style>
</head>
<body>
    ${content}
</body>
</html>`;
}

export function getNonce(): string {
    let text = '';
    const possible = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789';
    for (let i = 0; i < 32; i++) {
        text += possible.charAt(Math.floor(Math.random() * possible.length));
    }
    return text;
}
