---
phase: quick-12
plan: 12
type: execute
wave: 1
depends_on: []
files_modified:
  - frontend/src/components/cover-grid/album-dropdown.ts
autonomous: true
requirements: []
---

<objective>
Add a favorite (heart/star) icon to each track row in the album grid dropdown (`<album-dropdown>`), matching the existing pattern from `<track-list>`.

Purpose: Users can see at a glance which tracks are favorited and toggle favorites directly from the album dropdown, consistent with the track list view.
Output: Updated `album-dropdown.ts` with per-track favorite icon.
</objective>

<execution_context>
@/home/caleb/.config/opencode/get-shit-done/workflows/execute-plan.md
@/home/caleb/.config/opencode/get-shit-done/templates/summary.md
</execution_context>

<context>
@.planning/STATE.md
@frontend/src/components/cover-grid/album-dropdown.ts
@frontend/src/store/controllers/favorites-controller.ts

<interfaces>
<!-- Existing pattern from track-list.ts to replicate -->

From frontend/src/store/controllers/favorites-controller.ts:
```typescript
export class FavoritesController implements ReactiveController {
    constructor(host: ReactiveControllerHost);
    isFavorited(filePath: string): boolean;
    get iconName(): string;  // returns 'heart' or 'star'
    toggleFavorite(filePath: string): Promise<void>;
}
```

Existing favorite icon pattern from track-list.ts:
```typescript
// In the component class:
private favCtrl = new FavoritesController(this);

// In renderTrackRow():
const isFav = this.favCtrl.isFavorited(track.FilePath);
const favVariant = isFav ? 'solid' : 'regular';

// In the template, as the FIRST element in the track row:
<div
  class=${classMap({ 'fav-icon': true, favorited: isFav })}
  @click=${(e: MouseEvent) => {
      e.stopPropagation();
      void this.favCtrl.toggleFavorite(track.FilePath);
  }}
>
  <wa-icon
    name=${this.favCtrl.iconName}
    variant=${favVariant}
  ></wa-icon>
</div>
```

CSS for favorite icon (from track-list.ts):
```css
.fav-icon {
  display: flex; align-items: center; justify-content: center;
  width: 24px; flex-shrink: 0; cursor: pointer;
  color: var(--yj-text-tertiary, #666);
  font-size: var(--yj-text-sm);
  transition: color 0.1s ease;
}
.fav-icon:hover { color: var(--yj-text-primary, #fff); }
.fav-icon.favorited { color: var(--yj-accent, #ffd43b); }
.fav-icon.favorited:hover { color: var(--yj-accent, #ffd43b); opacity: 0.8; }
```
</interfaces>
</context>

<tasks>

<task type="auto">
  <name>Task 1: Add favorite icon to album dropdown track rows</name>
  <files>frontend/src/components/cover-grid/album-dropdown.ts</files>
  <action>
  Modify `album-dropdown.ts` to add a per-track favorite icon, following the exact pattern from `track-list.ts`:

  1. **Add imports:**
     - Import `FavoritesController` from `@store/controllers/favorites-controller`
     - Import `classMap` from `lit/directives/class-map.js`

  2. **Add controller instance** to the class body (next to the existing `player` controller):
     ```typescript
     private favCtrl = new FavoritesController(this);
     ```

  3. **Add CSS** for `.fav-icon` inside the existing `static override styles = css\`...\`` block, after the `.track-duration` rule. Use a compact sizing appropriate for the 12px font dropdown (use `width: 18px` instead of track-list's `24px`, and `font-size: 11px` to be proportional to the 12px track rows):
     ```css
     .fav-icon {
         display: flex;
         align-items: center;
         justify-content: center;
         width: 18px;
         flex-shrink: 0;
         cursor: pointer;
         color: var(--yj-text-tertiary, #666);
         font-size: 11px;
         transition: color 0.1s ease;
     }
     .fav-icon:hover {
         color: var(--yj-text-primary, #fff);
     }
     .fav-icon.favorited {
         color: var(--yj-accent, #ffd43b);
     }
     .fav-icon.favorited:hover {
         color: var(--yj-accent, #ffd43b);
         opacity: 0.8;
     }
     ```

  4. **Update `renderTrackRow()`** to add the favorite icon BETWEEN the track number and the track title. Compute `isFav` and `favVariant` at the top of the method, then insert the icon element:
     ```typescript
     const isFav = this.favCtrl.isFavorited(track.FilePath);
     const favVariant = isFav ? 'solid' : 'regular';
     ```
     Insert after `<span class="track-number">` and before `<span class="track-title">`:
     ```html
     <div
         class=${classMap({ 'fav-icon': true, favorited: isFav })}
         @click=${(e: MouseEvent) => {
             e.stopPropagation();
             void this.favCtrl.toggleFavorite(track.FilePath);
         }}
     >
         <wa-icon
             name=${this.favCtrl.iconName}
             variant=${favVariant}
         ></wa-icon>
     </div>
     ```

  **Important:** The click handler MUST call `e.stopPropagation()` to prevent the track-row click handler from also firing when toggling favorites.
  </action>
  <verify>
  Run: `cd frontend && npx tsc --noEmit`
  Verify: TypeScript compilation passes with no errors in album-dropdown.ts.
  </verify>
  <done>
  - Album dropdown track rows display a heart/star icon (matching user's configured icon style) between the track number and title
  - Favorited tracks show the icon in accent color (solid variant)
  - Non-favorited tracks show a subtle tertiary-colored icon (regular variant)
  - Clicking the icon toggles the favorite state without triggering track selection
  - Icon reactively updates when favorite state changes (via FavoritesController subscription)
  </done>
</task>

</tasks>

<verification>
`cd frontend && npx tsc --noEmit` — full TypeScript check passes
</verification>

<success_criteria>
- Favorite icon visible in album dropdown track rows
- Icon matches configured style (heart or star)
- Favorited state reflected visually (solid + accent color vs regular + tertiary)
- Click toggles favorite without selecting/playing track
- No TypeScript errors
</success_criteria>

<output>
After completion, create `.planning/quick/12-add-favorite-icon-to-album-grid-track-li/12-SUMMARY.md`
</output>
