---
phase: quick-9
plan: 01
type: execute
wave: 1
depends_on: []
files_modified:
  - frontend/src/components/queue-panel/queue-panel.ts
autonomous: true
must_haves:
  truths:
    - "Scrollbar tracks mouse position 1:1 when dragging DOWN on 20k+ track queue"
    - "Scrollbar still tracks mouse 1:1 when dragging UP"
    - "Queue items still display correctly (title, artist, position number)"
  artifacts:
    - path: "frontend/src/components/queue-panel/queue-panel.ts"
      provides: "Fixed-height queue track items for stable virtualizer scroll size"
  key_links:
    - from: "queue-panel .track-item CSS"
      to: "lit-virtualizer flow layout _scrollSize"
      via: "Fixed item height ensures stable average size calculation"
      pattern: "height:.*px.*overflow.*hidden"
---

<objective>
Fix queue panel scrollbar not following mouse 1:1 when dragging down on large queues (20k+ tracks).

Purpose: The root cause is lit-virtualizer's flow layout dynamically recalculating `_scrollSize` based on measured item averages. With 20k items but only ~15-20 measured at any time, the initial item size estimate (100px default) vs actual size (~48px) causes the scroll height to shrink dramatically as items get measured during downward scrolling. This makes the scrollbar thumb "lag" behind the mouse because the scroll container height keeps changing underneath the drag. Going UP works because those items are already measured and stable.

The fix is to set a fixed, explicit height on `.track-item` elements so that all items have identical measured heights from the very first render. This makes `_scrollSize = items.length * (averageMargin + averageSize) + averageMargin` completely stable because the average never changes — it equals the actual size of every item.

Output: Stable scrollbar behavior on large queues.
</objective>

<execution_context>
@/home/caleb/.config/Claude/get-shit-done/workflows/execute-plan.md
@/home/caleb/.config/Claude/get-shit-done/templates/summary.md
</execution_context>

<context>
@frontend/src/components/queue-panel/queue-panel.ts
@frontend/src/styles/tokens.css.ts

<interfaces>
<!-- lit-virtualizer flow layout internals (read-only, in node_modules) -->
<!-- DO NOT modify these files — understanding only -->

From node_modules/@lit-labs/virtualizer/layouts/flow.js:
```javascript
// _scrollSize is computed from average of MEASURED items only:
_updateScrollSize() {
    const { averageMarginSize } = this._metricsCache;
    this._scrollSize = Math.max(1, 
        this.items.length * (averageMarginSize + this._getAverageSize()) + averageMarginSize);
}

// Initial estimate before any measurements:
this._itemSize = { width: 100, height: 100 };  // <-- way off from actual ~48px

// Average comes from SizeCache which only has measured (visible) items:
_getAverageSize() {
    return this._metricsCache.averageChildSize || this._itemSize[this._sizeDim];
}
```

From frontend/src/styles/tokens.css.ts:
```typescript
--yj-text-xs: 11px;   // artist font
--yj-text-sm: 12px;   // position number font
--yj-text-md: 13px;   // title font
```
</interfaces>
</context>

<tasks>

<task type="auto">
  <name>Task 1: Set fixed height on queue track items and contain overflow</name>
  <files>frontend/src/components/queue-panel/queue-panel.ts</files>
  <action>
In the `queue-panel.ts` static styles, add a fixed `height` and `overflow: hidden` to the `.track-item` CSS rule. This ensures every queue item has an identical pixel height, which makes lit-virtualizer's `_scrollSize` calculation stable from the first render (the average of N identical measurements equals the measurement itself).

**Current `.track-item` CSS** (around line 273):
```css
.track-item {
    position: relative;
    display: flex;
    align-items: center;
    padding: 8px 16px;
    gap: 12px;
    border-bottom: 1px solid var(--yj-hover-overlay, rgba(255, 255, 255, 0.05));
    cursor: default;
    user-select: none;
    width: 100%;
    box-sizing: border-box;
}
```

**Add these properties to `.track-item`:**
```css
    height: 49px;
    overflow: hidden;
```

Height calculation: The content is two text lines (13px title at ~1.2 line-height = ~16px, 11px artist at ~1.2 line-height = ~13px) with 2px gap = ~31px content. Add 8px + 8px vertical padding = 47px. Plus 1px border-bottom = 48px total box. Setting `height: 49px` gives 1px breathing room for sub-pixel rounding (the `border-bottom` is outside the height due to `box-sizing: border-box` including it — actually border-box INCLUDES the border in height, so 49px = 8px pad-top + ~32px content + 8px pad-bottom + 1px border = 49px total). 

**IMPORTANT:** After setting this, verify the actual rendered height matches by loading the app with a queue of tracks and inspecting a `.track-item` in DevTools. If the actual measured height differs from 49px, adjust accordingly. The critical requirement is that ALL items have the SAME fixed height — the exact value matters less than uniformity.

Also add `overflow: hidden` on `.track-details` to ensure long titles/artists don't cause any height variation:
```css
.track-details {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 2px;
    overflow: hidden;  /* add this */
}
```

**Do NOT:**
- Change the flow layout or virtualizer configuration — the fix is pure CSS
- Add `min-height` or `max-height` — use only `height` for exact sizing
- Change padding, gap, or font sizes — only add the `height` and `overflow` properties
  </action>
  <verify>
    <automated>cd frontend && npx tsc --noEmit 2>&1 | head -20</automated>
  </verify>
  <done>
    - `.track-item` has explicit fixed `height: 49px` and `overflow: hidden`
    - `.track-details` has `overflow: hidden`
    - All queue items render at identical pixel heights
    - TypeScript compiles without errors
  </done>
</task>

<task type="checkpoint:human-verify" gate="blocking">
  <what-built>Fixed-height queue items to stabilize virtualizer scroll size estimation on large queues</what-built>
  <how-to-verify>
    1. Start the app with a large queue (20k+ tracks)
    2. Click the scrollbar thumb in the queue panel and drag it DOWNWARD slowly
    3. Verify the scrollbar follows your mouse position 1:1 (no lag, no fixed-speed movement)
    4. Drag the scrollbar UP — verify it still follows 1:1 (regression check)
    5. Scroll rapidly up and down — verify smooth, consistent behavior
    6. Verify track items still look correct (no clipped text, proper spacing)
    7. Inspect a `.track-item` in DevTools — confirm all visible items have identical height (49px or whatever the final value is)
    8. If the items look too cramped or too tall, adjust the `height` value and re-test
  </how-to-verify>
  <resume-signal>Type "approved" or describe any remaining scroll issues or visual problems</resume-signal>
</task>

</tasks>

<verification>
- Queue panel scrollbar tracks mouse 1:1 in both directions on 20k+ track queue
- No visual regression in track item appearance
- TypeScript compiles cleanly
</verification>

<success_criteria>
- Scrollbar follows mouse position proportionally when dragging in both directions
- Works correctly on queues with 20k+ tracks
- No visual layout changes to queue track items
</success_criteria>

<output>
After completion, create `.planning/quick/9-fix-queue-panel-scroll-bar-not-following/9-SUMMARY.md`
</output>
