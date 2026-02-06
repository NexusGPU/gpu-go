import * as assert from 'assert';
import { resolveAuthMode } from '../../utils/authState';

suite('authState', () => {
    test('guest mode wins when not logged in', () => {
        const mode = resolveAuthMode({ loggedIn: false, guestMode: true });
        assert.strictEqual(mode, 'guest');
    });

    test('login wins over guest', () => {
        const mode = resolveAuthMode({ loggedIn: true, guestMode: true });
        assert.strictEqual(mode, 'full');
    });
});
