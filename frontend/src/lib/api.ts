// Centralized API base URL resolver.
// Order:
// 1) Respect build-time VITE_API_BASE_URL if provided.
// 2) If running under a frontend domain like vod-tender.<domain>, map to vod-api.<domain>.
// 3) Fallback to same-origin (useful if reverse proxy routes / to API).
export function getApiBase(): string {
    const env = (import.meta.env ?? {});
    const fromEnv = env.VITE_API_BASE_URL as string | undefined;
    if (fromEnv && fromEnv.trim() !== '') return fromEnv.trim();
    if (typeof window !== 'undefined') {
        const url = new URL(window.location.href);
        const host = url.host;
        if (host.startsWith('vod-tender.')) {
            const apiHost = host.replace('vod-tender.', 'vod-api.');
            return `${url.protocol}//${apiHost}`;
        }
        return `${url.protocol}//${host}`;
    }
    return '';
}
