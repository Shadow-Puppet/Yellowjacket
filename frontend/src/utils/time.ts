/**
 * Format seconds into mm:ss format
 */
export function formatSeconds(totalSeconds: number): string {
    const minutes = Math.floor(totalSeconds / 60);
    const seconds = Math.floor(totalSeconds % 60);
    return `${minutes.toString().padStart(2, '0')}:${seconds.toString().padStart(2, '0')}`;
}

/**
 * Format milliseconds into mm:ss format
 * Accepts number or string, returns '--:--' if invalid
 */
export function formatMilliseconds(ms: number | string): string {
    const millis = typeof ms === 'string' ? parseInt(ms) : ms;
    if (isNaN(millis)) return '--:--';
    return formatSeconds(Math.floor(millis / 1000));
}
