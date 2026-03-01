---
phase: quick-006
plan: 1
type: execute
wave: 1
depends_on: []
files_modified:
  - frontend/src/components/playlist-view/playlist-view.ts
autonomous: true
requirements: [QUICK-006]

must_haves:
  truths:
    - "Playlist entries in the playlist list do NOT show a 'list' icon before the name"
    - "The default (favorites) playlist entry shows a heart or star icon (matching favoritesStore iconStyle) instead of no icon"
    - "Non-default playlists show no icon between the chevron and the name"
  artifacts:
    - path: "frontend/src/components/playlist-view/playlist-view.ts"
      provides: "Updated playlist item rendering without list icon, with favorites icon for default playlist"
  key_links:
    - from: "renderPlaylistItem"
      to: "favCtrl.playlistId / favCtrl.iconName"
      via: "Conditional icon rendering based on default playlist ID match"
      pattern: "favCtrl\\.playlistId|favCtrl\\.iconName"
---

<objective>
Remove the "list" icon that appears before every playlist name in the playlist view, and add the user-configured favorites icon (heart or star) to the default playlist entry only.

Purpose: Cleaner playlist list — the list icon adds visual noise; the favorites icon on the default playlist gives quick visual identification.
Output: Updated playlist-view.ts with conditional icon rendering.
</objective>

<execution_context>
@.planning/quick/6-remove-list-icon-from-playlist-names-and/6-PLAN.md
</execution_context>

<context>
@frontend/src/components/playlist-view/playlist-view.ts (main file to modify)
@frontend/src/store/controllers/favorites-controller.ts (provides favCtrl.playlistId, favCtrl.iconName)

<interfaces>
From frontend/wailsjs/go/models.ts (playlist namespace):
```typescript
export class Summary {
    ID: number;
    Name: string;
    CreatedAt: string;
    UpdatedAt: string;
}
```

From frontend/src/store/controllers/favorites-controller.ts:
```typescript
// Already instantiated on the component as: private favCtrl = new FavoritesController(this);
get playlistId(): number;    // Returns the default playlist's DB ID
get iconName(): string;      // Returns 'star' or 'heart' based on user config
```
</interfaces>
</context>

<tasks>

<task type="auto">
  <name>Task 1: Remove list icon from all playlists and add favorites icon to default playlist</name>
  <files>frontend/src/components/playlist-view/playlist-view.ts</files>
  <action>
In the `renderPlaylistItem` method (~line 2883), replace the static `<wa-icon class="playlist-icon" name="list"></wa-icon>` block (lines 2923-2926) with a conditional:

- If `entry.summary.ID === this.favCtrl.playlistId`, render `<wa-icon class="playlist-icon" name=${this.favCtrl.iconName}></wa-icon>` (shows heart or star per user config)
- Otherwise, render nothing (no icon at all between chevron and name)

The existing `.playlist-icon` CSS class (lines 568-572) should remain — it styles the icon for the default playlist entry. No CSS changes needed.

Also update the `.playlist-body` left padding from `42px` to `32px` (line 590) to tighten the track list indentation now that most rows no longer have the icon taking up ~28px (18px icon + 10px gap). This keeps the tracks visually aligned under the playlist name rather than indented too far.

Do NOT touch the empty-state `<wa-icon name="list">` on line 2818 — that's the "no playlists" illustration, not a per-playlist icon.
  </action>
  <verify>
    npm run --prefix frontend check (TypeScript compiles without errors)
  </verify>
  <done>
    - No playlist entry shows the "list" icon
    - The default/favorites playlist entry shows the heart or star icon (matching user config)
    - Non-default playlists show only the chevron then the name (no icon between)
    - TypeScript compiles cleanly
  </done>
</task>

</tasks>

<verification>
- `npm run --prefix frontend check` passes
- Visual: In the playlist view, non-default playlists show chevron → name (no icon). The default playlist shows chevron → heart/star → name.
</verification>

<success_criteria>
The list icon is removed from all playlist entries. The default playlist entry displays the user-configured favorites icon (heart or star). All other playlists show no icon. TypeScript compiles without errors.
</success_criteria>

<output>
After completion, create `.planning/quick/6-remove-list-icon-from-playlist-names-and/6-SUMMARY.md`
</output>
