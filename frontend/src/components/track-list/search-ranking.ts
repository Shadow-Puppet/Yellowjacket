import type { library } from '@go/models';
import { html } from 'lit';
import type { TemplateResult } from 'lit';

import {
    COLUMN_DEFS,
    CORE_SEARCH_COLUMN_IDS,
} from './columns';
import type { ColumnDef } from './columns';

// =================================================================
// Field weights — higher means more relevant when matched
// =================================================================

const FIELD_WEIGHTS: Record<string, number> = {
    trackName: 100,
    artistName: 80,
    album: 60,
    composer: 40,
    genre: 40,
    year: 20,
    filePath: 20,
    fileType: 20,
    trackNumber: 20,
    discNumber: 20,
    sampleRate: 20,
    bitDepth: 20,
    channels: 20,
    bitrate: 20,
    fileSize: 20,
    trackLength: 20,
};

// =================================================================
// Match quality multipliers
// =================================================================

/** Entire field value equals the search term. */
const EXACT_MATCH = 4;

/** Field value starts with the search term. */
const PREFIX_MATCH = 3;

/** Term appears at a word boundary within the field. */
const WORD_BOUNDARY_MATCH = 2;

/** Term is a substring somewhere in the field. */
const CONTAINS_MATCH = 1;

/**
 * Pattern that matches common word-boundary characters.
 * Used to test whether a substring match sits at the start of a
 * "word" inside the field value.
 */
const WORD_BOUNDARY = /[\s\-_(/[\].,;:!?'"]/;

// =================================================================
// Scoring
// =================================================================

/**
 * Compute the match quality multiplier for a single field value
 * against the lowercased search term.
 *
 * @returns The quality multiplier (1–4), or 0 if no match.
 */
function matchQuality(
    fieldLower: string,
    termLower: string,
): number {
    if (fieldLower === termLower) return EXACT_MATCH;
    if (fieldLower.startsWith(termLower)) return PREFIX_MATCH;

    const idx = fieldLower.indexOf(termLower);

    if (idx === -1) return 0;

    // Check if the character before the match is a word boundary.
    if (
        idx > 0 &&
        WORD_BOUNDARY.test(fieldLower[idx - 1]!)
    ) {
        return WORD_BOUNDARY_MATCH;
    }

    return CONTAINS_MATCH;
}

/**
 * Score a single track against a search term.
 *
 * The score is the best `fieldWeight × matchQuality` across all
 * searchable fields.  Returns 0 if no field matches (the track
 * should be filtered out).
 *
 * @param track      The track to score.
 * @param termLower  The search term, already lowercased.
 * @param columns    The set of column defs to search.  Core search
 *                   fields are always included on top of these.
 */
function scoreTrack(
    track: library.Track,
    termLower: string,
    columns: ColumnDef[],
): number {
    let best = 0;

    // Build the deduplicated set of column IDs to check.
    const seen = new Set<string>();

    const check = (col: ColumnDef) => {
        if (seen.has(col.id)) return;
        seen.add(col.id);

        const value = col.accessor(track).toLowerCase();

        if (!value) return;

        const quality = matchQuality(value, termLower);

        if (quality === 0) return;

        const weight = FIELD_WEIGHTS[col.id] ?? 20;
        const score = weight * quality;

        if (score > best) best = score;
    };

    // Always search core fields first.
    for (const id of CORE_SEARCH_COLUMN_IDS) {
        const col = COLUMN_DEFS[id];

        if (col) check(col);
    }

    // Then search any additional visible columns.
    for (const col of columns) {
        check(col);
    }

    return best;
}

// =================================================================
// Public API
// =================================================================

/** A track paired with its relevance score. */
export interface RankedTrack {
    track: library.Track;
    score: number;
}

/**
 * Filter and rank tracks by relevance to a search term.
 *
 * Tracks that don't match any searchable field are excluded.
 * The returned array is sorted descending by score (best match
 * first).  A companion `Map` of FilePath → score is also returned
 * so that `computeSortedTracks` can use relevance as a tiebreaker.
 *
 * @param tracks         The full, unfiltered track list.
 * @param term           The raw search term (will be lowercased).
 * @param activeColumns  Currently visible column definitions.
 * @returns An object with `tracks` (filtered & ranked) and
 *          `scores` (Map of FilePath → relevance score).
 */
export function rankTracks(
    tracks: library.Track[],
    term: string,
    activeColumns: ColumnDef[],
): { tracks: library.Track[]; scores: Map<string, number> } {
    const termLower = term.toLowerCase();
    const ranked: RankedTrack[] = [];

    for (const track of tracks) {
        const score = scoreTrack(
            track,
            termLower,
            activeColumns,
        );

        if (score > 0) {
            ranked.push({ track, score });
        }
    }

    // Sort descending by score (highest relevance first).
    ranked.sort((a, b) => b.score - a.score);

    const result: library.Track[] = [];
    const scores = new Map<string, number>();

    for (const r of ranked) {
        result.push(r.track);
        scores.set(r.track.FilePath, r.score);
    }

    return { tracks: result, scores };
}

// =================================================================
// Search term highlighting
// =================================================================

/**
 * Highlight all occurrences of a search term within a text value.
 *
 * Returns a Lit `TemplateResult` with matched substrings wrapped in
 * `<span class="search-match">`.  The matching is case-insensitive.
 * If the term is empty or not found, the original string is returned
 * as-is (no wrapper elements).
 *
 * @param text  The cell display value.
 * @param term  The raw search term.
 */
export function highlightText(
    text: string,
    term: string,
): string | TemplateResult {
    if (!term) return text;

    const termLower = term.toLowerCase();
    const textLower = text.toLowerCase();
    const firstIdx = textLower.indexOf(termLower);

    if (firstIdx === -1) return text;

    const parts: (string | TemplateResult)[] = [];
    let cursor = 0;

    let idx = firstIdx;

    while (idx !== -1) {
        // Text before the match.
        if (idx > cursor) {
            parts.push(text.slice(cursor, idx));
        }

        // The matched substring (preserving original case).
        const matched = text.slice(
            idx,
            idx + term.length,
        );

        parts.push(
            html`<span class="search-match"
                >${matched}</span
            >`,
        );

        cursor = idx + term.length;
        idx = textLower.indexOf(termLower, cursor);
    }

    // Remaining text after the last match.
    if (cursor < text.length) {
        parts.push(text.slice(cursor));
    }

    return html`${parts}`;
}
