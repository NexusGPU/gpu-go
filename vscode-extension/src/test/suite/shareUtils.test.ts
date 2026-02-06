import * as assert from 'assert';
import { groupSharesByWorker } from '../../utils/shareUtils';

suite('shareUtils', () => {
    test('groups shares by worker and keeps top 10 newest first', () => {
        const shares = Array.from({ length: 12 }).map((_, i) => ({
            shareId: `s${i}`,
            shortCode: `c${i}`,
            shortLink: `https://go.gpu.tf/s/c${i}`,
            workerId: 'w1',
            hardwareVendor: 'nvidia',
            connectionUrl: `https://1.2.3.4:${9000 + i}`,
            usedCount: 0,
            createdAt: new Date(2026, 0, i + 1).toISOString()
        }));

        const grouped = groupSharesByWorker(shares, 10);
        assert.strictEqual(grouped.get('w1')?.length, 10);
        assert.strictEqual(grouped.get('w1')?.[0].shortCode, 'c11');
    });
});
