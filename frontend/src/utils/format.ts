/** Em-dash used for unknown/zero values. */
const UNKNOWN = '\u2014';

/**
 * Format a sample rate in Hz to a human-readable string.
 * Returns "44.1 kHz", "48 kHz", "96 kHz", etc.
 * Returns an em-dash for zero or falsy values.
 */
export function formatSampleRate(hz: number): string {
    if (!hz) return UNKNOWN;

    const khz = hz / 1000;

    // Display as integer if it's a whole number, otherwise
    // one decimal place (e.g. 44.1 kHz).
    const formatted =
        khz % 1 === 0 ? khz.toString() : khz.toFixed(1);

    return `${formatted} kHz`;
}

/**
 * Format bit depth (bits per sample) to a human-readable string.
 * Returns "16-bit", "24-bit", "32-bit", etc.
 * Returns an em-dash for zero or falsy values.
 */
export function formatBitDepth(bits: number): string {
    if (!bits) return UNKNOWN;

    return `${bits}-bit`;
}

/**
 * Format a channel count to a human-readable string.
 * Returns "Mono", "Stereo", or "N ch" for other counts.
 * Returns an em-dash for zero or falsy values.
 */
export function formatChannels(n: number): string {
    if (!n) return UNKNOWN;
    if (n === 1) return 'Mono';
    if (n === 2) return 'Stereo';

    return `${n} ch`;
}

/**
 * Format a bitrate in kbps to a human-readable string.
 * Returns "320 kbps", "1,411 kbps", etc.
 * Returns an em-dash for zero or falsy values.
 */
export function formatBitrate(kbps: number): string {
    if (!kbps) return UNKNOWN;

    return `${kbps.toLocaleString()} kbps`;
}

/**
 * Format a file size in bytes to a human-readable string.
 * Uses binary units: KiB, MiB, GiB.
 * Returns an em-dash for zero or falsy values.
 */
export function formatFileSize(bytes: number): string {
    if (!bytes) return UNKNOWN;

    const kib = 1024;
    const mib = kib * 1024;
    const gib = mib * 1024;

    if (bytes >= gib) {
        return `${(bytes / gib).toFixed(1)} GB`;
    }

    if (bytes >= mib) {
        return `${(bytes / mib).toFixed(1)} MB`;
    }

    if (bytes >= kib) {
        return `${(bytes / kib).toFixed(1)} KB`;
    }

    return `${bytes} B`;
}
