import { Share } from '../cli/cli';

function timestamp(value: string | undefined): number {
    if (!value) {
        return 0;
    }
    const parsed = new Date(value).getTime();
    return Number.isNaN(parsed) ? 0 : parsed;
}

export function groupSharesByWorker(shares: Share[], limit: number): Map<string, Share[]> {
    const grouped = new Map<string, Share[]>();

    for (const share of shares) {
        const list = grouped.get(share.workerId) ?? [];
        list.push(share);
        grouped.set(share.workerId, list);
    }

    for (const [workerId, list] of grouped.entries()) {
        list.sort((a, b) => timestamp(b.createdAt) - timestamp(a.createdAt));
        grouped.set(workerId, list.slice(0, limit));
    }

    return grouped;
}
