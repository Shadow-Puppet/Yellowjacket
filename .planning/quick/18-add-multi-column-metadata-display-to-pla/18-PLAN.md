---
phase: quick-18
plan: 1
type: execute
wave: 1
depends_on: []
files_modified:
  - frontend/src/components/playlist-details/playlist-details.ts
autonomous: true
requirements: [QUICK-18]

must_haves:
  truths:
    - "Playlist tracks display in a multi-column grid with header row showing #, Title, Artist, Album, Duration"
    - "Each track row shows a 1-indexed playlist order number in the # column"
    - "Phantom tracks still render with their special warning/path/action layout spanning the full row"
    - "Column text truncates with ellipsis when too narrow, matching track-list visual style"
  artifacts:
    - path: "frontend/src/components/playlist-details/playlist-details.ts"
      provides: "Multi-column grid track display with playlist order numbers"
      contains: "grid-template-columns"
  key_links:
    - from: "playlist-details.ts renderTrackList"
      to: "playlist.Track fields"
      via: "track.Title, track.Artist, track.Album, track.Duration"
      pattern: "grid-template-columns"
---

<objective>
Replace the single-row `<track-info>` based track rendering in playlist-details with a multi-column grid layout matching track-list's column style, and add a playlist order number column.

Purpose: Make playlist track display consistent with the main track-list view — users see the same columnar metadata (title, artist, album, duration) plus a # column showing playlist position.
Output: Updated playlist-details.ts with grid-based track rows and column header.
</objective>

<execution_context>
@/home/caleb/.config/Claude/get-shit-done/workflows/execute-plan.md
@/home/caleb/.config/Claude/get-shit-done/templates/summary.md
</execution_context>

<context>
@frontend/src/components/playlist-details/playlist-details.ts
@frontend/src/components/track-list/track-list.ts (reference for grid styles)
@frontend/src/components/track-list/columns.ts (reference for column patterns)

<interfaces>
<!-- playlist.Track type (from wailsjs/go/models.ts, namespace playlist) -->
```typescript
export class Track {
    ID: number;
    Position: number;
    FilePath: string;
    Title: string;
    Artist: string;
    Album: string;
    CoverArtPath: string;
    CoverArtSmall: string;
    CoverArtMedium: string;
    CoverArtLarge: string;
    Duration: string;
    Phantom: boolean;
}
```

<!-- formatMilliseconds utility -->
```typescript
// from @utils/time
export function formatMilliseconds(ms: number | string): string;
```
</interfaces>
</context>

<tasks>

<task type="auto">
  <name>Task 1: Replace track-info rendering with multi-column grid layout</name>
  <files>frontend/src/components/playlist-details/playlist-details.ts</files>
  <action>
Modify playlist-details.ts to display tracks in a multi-column grid instead of using the `<track-info>` component:

**1. Remove track-info import:**
Remove `import '@components/track-info/track-info';` — it will no longer be used.

**2. Add formatMilliseconds import:**
Add `import { formatMilliseconds } from '@utils/time';` (needed to format Duration for display).

**3. Add column header row in `renderTrackList()`:**
After the `.playlist-actions` div and before the track map, add a header row:
```html
<div class="track-header">
    <div class="header-cell col-number">#</div>
    <div class="header-cell col-title">Title</div>
    <div class="header-cell col-artist">Artist</div>
    <div class="header-cell col-album">Album</div>
    <div class="header-cell col-duration">Duration</div>
</div>
```

**4. Replace `<track-info>` in the non-phantom branch of track rendering:**
Currently renders:
```html
<track-info
    .trackTitle=${track.Title || track.FilePath}
    .artist=${track.Artist}
    .duration=${track.Duration}
    .filePath=${track.FilePath}
></track-info>
```

Replace with inline grid cells:
```html
<span class="cell col-number">${trackIndex + 1}</span>
<span class="cell col-title" title="${track.Title || track.FilePath}">${track.Title || track.FilePath}</span>
<span class="cell col-artist" title="${track.Artist}">${track.Artist}</span>
<span class="cell col-album" title="${track.Album}">${track.Album}</span>
<span class="cell col-duration">${formatMilliseconds(track.Duration)}</span>
```

**5. Update phantom track rendering to span the grid:**
Wrap the existing phantom-row content so it spans all 5 columns. Add `style="grid-column: 1 / -1"` to the `.phantom-row` div so it spans the full width of the grid.

**6. Add CSS grid styles:**
Add these styles to the static styles (either replacing or augmenting `.track-item`):

```css
/* Column grid layout */
.track-header,
.track-item {
    display: grid;
    grid-template-columns: 40px 1fr 1fr 1fr 80px;
    align-items: center;
    gap: 0;
}

.track-header {
    padding: 6px 8px;
    font-size: 11px;
    font-weight: 600;
    color: var(--yj-text-secondary, #b3b3b3);
    text-transform: uppercase;
    letter-spacing: 0.03em;
    border-bottom: 1px solid var(--yj-text-tertiary, #666);
    user-select: none;
}

.track-item {
    padding: 6px 8px;
}

.header-cell,
.cell {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    min-width: 0;
    padding: 0 4px;
}

.col-number {
    text-align: center;
    color: var(--yj-text-tertiary, #888);
    font-variant-numeric: tabular-nums;
}

.col-duration {
    text-align: right;
    color: var(--yj-text-tertiary, #888);
    font-variant-numeric: tabular-nums;
}

/* Phantom rows span full grid */
.track-item.phantom {
    display: grid;
    grid-template-columns: 40px 1fr 1fr 1fr 80px;
}

.phantom-row {
    grid-column: 1 / -1;
}
```

Remove the old `.track-item { padding: 6px 0; }` rule since `.track-item` now has `padding: 6px 8px` from the grid styles. Keep all existing `.track-item.selected`, `.track-item.active`, `.track-item.phantom` background/color rules.

**Important:** The `.track-item` selector already exists with `padding: 6px 0;` and other rules. Merge the grid properties into the existing rule (change padding from `6px 0` to `6px 8px`, add `display: grid`, `grid-template-columns`, `align-items: center`, `gap: 0`). Do NOT duplicate the selector.

**7. Remove `.track-item:last-child { border-bottom: none; }` rule** — keep consistent grid borders.

No need to add the `content` wrapper with `overflow-y: auto` around the track list since `.content` already handles scrolling.
  </action>
  <verify>cd frontend && npx tsc --noEmit</verify>
  <done>
    - Playlist tracks display in a 5-column grid: #, Title, Artist, Album, Duration
    - Header row shows column labels styled like track-list headers
    - Order number column shows 1-indexed position (trackIndex + 1)
    - Phantom tracks still render correctly with warning icon/path/actions spanning full width
    - All existing interactions preserved (click, dblclick, contextmenu, drag)
    - TypeScript compiles without errors
  </done>
</task>

</tasks>

<verification>
- `cd frontend && npx tsc --noEmit` — no type errors
- Visual: playlist-details shows columnar layout with #, Title, Artist, Album, Duration header
- Visual: track rows align to the grid columns with text truncation on narrow columns
- Visual: phantom tracks still display with warning icon and full-width path
- Functional: all click/dblclick/context menu/drag interactions still work
</verification>

<success_criteria>
- Playlist tracks render in multi-column grid matching track-list visual style
- # column shows 1-indexed playlist order numbers
- Header row visible with column labels
- Phantom tracks render correctly spanning full width
- No TypeScript errors
</success_criteria>

<output>
After completion, create `.planning/quick/18-add-multi-column-metadata-display-to-pla/18-SUMMARY.md`
</output>
