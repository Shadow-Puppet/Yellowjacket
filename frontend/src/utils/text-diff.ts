// Inline text diff for the autotag review UI.  Splits both sides
// into tokens (word runs, whitespace runs, individual punctuation
// chars), runs LCS, and emits a flat segment list the renderer
// drops into spans.  Punctuation is its own token so an apostrophe
// type swap (' vs ') shows just the apostrophe as changed instead
// of the whole word — that's the case the visible-but-identical
// titles in the autotag view were tripping on.

export type SegmentType = 'equal' | 'remove' | 'add';

export interface DiffSegment {
    type: SegmentType;
    text: string;
}

const tokenRe = /(\w+|\s+|[^\w\s])/g;

function tokenize(s: string): string[] {
    return s.match(tokenRe) ?? [];
}

/**
 * Compute an inline word/punct-level diff between `a` (old) and
 * `b` (new), returning a list of segments suitable for inline
 * rendering: equal segments come from both sides, remove segments
 * come from `a` only, add segments come from `b` only.  Adjacent
 * segments of the same type are coalesced.  Both sides empty
 * returns a single empty equal segment.
 */
export function inlineDiff(a: string, b: string): DiffSegment[] {
    if (a === b) {
        return [{ type: 'equal', text: a }];
    }
    if (a === '') {
        return [{ type: 'add', text: b }];
    }
    if (b === '') {
        return [{ type: 'remove', text: a }];
    }

    const ta = tokenize(a);
    const tb = tokenize(b);
    const m = ta.length;
    const n = tb.length;

    // LCS table — O(m*n) memory.  Track titles cap out at ~100
    // tokens so this stays trivially small.
    const dp: number[][] = Array.from({ length: m + 1 }, () =>
        new Array<number>(n + 1).fill(0),
    );
    for (let i = 1; i <= m; i++) {
        for (let j = 1; j <= n; j++) {
            if (ta[i - 1] === tb[j - 1]) {
                dp[i]![j] = dp[i - 1]![j - 1]! + 1;
            } else {
                dp[i]![j] = Math.max(dp[i - 1]![j]!, dp[i]![j - 1]!);
            }
        }
    }

    // Backtrack to build the op list (reversed).
    const ops: DiffSegment[] = [];
    let i = m;
    let j = n;
    while (i > 0 || j > 0) {
        if (i > 0 && j > 0 && ta[i - 1] === tb[j - 1]) {
            ops.push({ type: 'equal', text: ta[i - 1]! });
            i--;
            j--;
        } else if (j > 0 && (i === 0 || dp[i]![j - 1]! >= dp[i - 1]![j]!)) {
            ops.push({ type: 'add', text: tb[j - 1]! });
            j--;
        } else {
            ops.push({ type: 'remove', text: ta[i - 1]! });
            i--;
        }
    }
    ops.reverse();

    // Coalesce adjacent same-type segments so the renderer outputs
    // one span per visual run instead of per token.
    const merged: DiffSegment[] = [];
    for (const seg of ops) {
        const last = merged[merged.length - 1];
        if (last && last.type === seg.type) {
            last.text += seg.text;
        } else {
            merged.push({ type: seg.type, text: seg.text });
        }
    }
    return merged;
}
