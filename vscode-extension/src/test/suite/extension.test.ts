import * as assert from 'assert';
import * as vscode from 'vscode';

suite('Extension Test Suite', () => {
    vscode.window.showInformationMessage('Start all tests.');

    test('Extension should be present', () => {
        assert.ok(vscode.extensions.getExtension('tensor-fusion.gpu-go'));
    });

    test('Extension should activate', async () => {
        const ext = vscode.extensions.getExtension('tensor-fusion.gpu-go');
        if (ext) {
            await ext.activate();
            assert.ok(ext.isActive);
        }
    });

    test('Commands should be registered', async () => {
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
