export type AuthMode = 'guest' | 'full' | 'none';

export function resolveAuthMode(input: { loggedIn: boolean; guestMode: boolean }): AuthMode {
    if (input.loggedIn) {
        return 'full';
    }
    if (input.guestMode) {
        return 'guest';
    }
    return 'none';
}
