/**
 * One map from a Go error to a sentence a person can act on.
 *
 * Eight places in this app used to render `err.Error()` verbatim, so a
 * user was shown `Get "https://musicbrainz.org/ws/2/…": context
 * deadline exceeded` (errors.M9). Those strings come out of
 * `net/http`, `database/sql` and `musicbrainzws2`; they are debugging
 * tools, not copy.
 *
 * The map is deliberately short. It recognises the causes a user can do
 * something about and says something generic about everything else,
 * because a confidently wrong diagnosis is worse than an honest shrug.
 * The raw text belongs in `console.error`, and every call site keeps it
 * there.
 */

/** The fallback when nothing matches: honest, and never a Go string. */
const GENERIC = 'Something went wrong.';

/**
 * Ordered, and the order matters: a client timeout says "canceled" and
 * a DNS failure says "not found", so the more specific cause has to be
 * tested first.
 */
const RULES: Array<[RegExp, string]> = [
    [
        /context deadline exceeded|client\.timeout|i\/o timeout|timed? ?out/i,
        'The request took too long — the server did not answer in time.',
    ],
    [
        /context canceled|context cancelled|operation was cancell?ed/i,
        'That was cancelled before it finished.',
    ],
    [
        /no such host|network is unreachable|connection refused|connection reset|dial tcp|no route to host|eai_again/i,
        'Could not reach the network. Check your connection and try again.',
    ],
    [
        /permission denied|access is denied|operation not permitted|eacces/i,
        'Permission denied — the app is not allowed to read or write there.',
    ],
    [
        /no such file or directory|cannot find the (file|path)|\b404\b|not found/i,
        'That could not be found — it may have been moved or deleted.',
    ],
    [
        /database is locked|database table is locked|sqlite_busy|resource busy|device or resource busy/i,
        'The library database is busy. Try again in a moment.',
    ],
    [
        /no space left on device|disk (is )?full|not enough space/i,
        'There is no space left on the disk.',
    ],
    [
        /read-only file system|read only file system/i,
        'That location is read-only.',
    ],
];

/** The text a rejected binding actually carries, whatever its shape. */
function messageOf(err: unknown): string {
    if (typeof err === 'string') return err;
    if (err instanceof Error) return err.message;

    if (typeof err === 'object' && err !== null && 'message' in err) {
        const { message } = err as { message: unknown };

        if (typeof message === 'string') return message;
    }

    return '';
}

/**
 * Describe a failure in a sentence.
 *
 * @param err      whatever the rejection carried.
 * @param fallback what to say when the cause is not one this knows;
 *                 pass something specific to the operation where the
 *                 caller knows more than this map does.
 */
export function describeError(err: unknown, fallback = GENERIC): string {
    const raw = messageOf(err);

    if (raw === '') return fallback;

    for (const [pattern, sentence] of RULES) {
        if (pattern.test(raw)) return sentence;
    }

    return fallback;
}

/**
 * Some backend errors *are* sentences already — the ones this app
 * writes itself, as sentinels, for conditions it defined ("a library
 * with that name already exists"). Those are worth showing; a Go
 * wrapping chain (`could not rename library: sql: …`) is not.
 *
 * A message qualifies when it is short and carries none of the markers
 * that mean it came from the runtime rather than from us.
 */
const GO_NOISE =
    /^(get|post|put|delete|head|patch) "|https?:\/\/|\bsql:|\bdial\b|\bexec\b|\bsyscall\b|goroutine |panic:|0x[0-9a-f]{6}|\.go:\d+|context (deadline|canceled|cancelled)|no such file or directory|\bEOF\b/i;

export function isPlainSentence(err: unknown): boolean {
    const raw = messageOf(err).trim();

    if (raw === '' || raw.length > 140) return false;

    return !GO_NOISE.test(raw);
}

/**
 * The sentence to show for a failure: the backend's own words when it
 * had any worth repeating, otherwise the mapped cause.
 */
export function explainError(err: unknown, fallback = GENERIC): string {
    if (isPlainSentence(err)) {
        const raw = messageOf(err).trim();

        return raw.endsWith('.') ? raw : `${raw}.`;
    }

    return describeError(err, fallback);
}
