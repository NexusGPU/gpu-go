import * as assert from 'assert';
import * as vscode from 'vscode';

suite('Extension Test Suite', () => {
    // Increase timeout for activation tests
    // Note: CLI download is disabled via GPUGO_SKIP_CLI_DOWNLOAD env var in runTest.ts
    const ACTIVATION_TIMEOUT = 10000;

    test('Extension should be present', () => {
        assert.ok(vscode.extensions.getExtension('nexusgpu.gpu-go'));
    });

    test('Extension should activate', async function() {
        this.timeout(ACTIVATION_TIMEOUT);
        
        const ext = vscode.extensions.getExtension('nexusgpu.gpu-go');
        assert.ok(ext, 'Extension should be found');
        
        if (!ext.isActive) {
            await ext.activate();
        }
        assert.ok(ext.isActive, 'Extension should be active');
    });

    test('Commands should be registered', async function() {
        this.timeout(ACTIVATION_TIMEOUT);
        
        // Ensure extension is activated first
        const ext = vscode.extensions.getExtension('nexusgpu.gpu-go');
        if (ext && !ext.isActive) {
            await ext.activate();
        }
        
        const commands = await vscode.commands.getCommands();
        
        const expectedCommands = [
            'gpugo.login',
            'gpugo.logout',
            'gpugo.refreshStudio',
            'gpugo.refreshWorkers',
            'gpugo.refreshDevices',
            'gpugo.createStudio',
            'gpugo.createWorker',
            'gpugo.openDashboard'
        ];

        for (const cmd of expectedCommands) {
            assert.ok(commands.includes(cmd), `Command ${cmd} should be registered`);
        }
    });
});
