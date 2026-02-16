# Fix Queue Panel Colors to Match Application

## Problem

The queue panel (`frontend/src/components/queue-panel/queue-panel.ts`) uses a blue-tinted dark background (`#1a1a2e`) and dimmer secondary text colors that don't match the rest of the application's Bootstrap-inspired neutral dark grey palette.

## Application Color Palette (established)

| Role | Color | Used by |
|------|-------|---------|
| Top bar / Bottom bar | `#343a40` | `index.css` |
| Sidebar / Main panel | `#212529` | `index.css`, `app-sidebar.ts` |
| Body background | `black` | `index.css` |
| Secondary text | `#b3b3b3` | `cover-grid.ts` (artist, empty state, loading) |
| Muted text | `#888` | various components |
| Accent | `#ffd43b` | all components (active/hover states) |

## Changes

All changes are in `frontend/src/components/queue-panel/queue-panel.ts`:

### 1. Background color (line 27)

```css
/* Before */
background-color: #1a1a2e;

/* After */
background-color: #212529;
```

**Reason**: `#1a1a2e` is blue-tinted (RGB 26,26,46). Should match sidebar & main panel neutral grey `#212529`.

### 2. `.track-position` color (line 119)

```css
/* Before */
color: #666;

/* After */
color: #888;
```

**Reason**: Slightly brighter to improve readability and match secondary text conventions.

### 3. `.track-artist` color (line 149)

```css
/* Before */
color: #888;

/* After */
color: #b3b3b3;
```

**Reason**: Match artist/secondary text color used in `cover-grid.ts`.

### 4. `.remove-button` color (line 158)

```css
/* Before */
color: #666;

/* After */
color: #888;
```

**Reason**: Slightly brighter for consistency with other muted interactive elements.

### 5. `.empty-state` color (line 181)

```css
/* Before */
color: #666;

/* After */
color: #b3b3b3;
```

**Reason**: Match empty-state color in `cover-grid.ts`.

## No changes needed

These properties already match the rest of the app:
- Border colors (`#333`) - used consistently
- Resize handle hover (`#6c757d`) - matches sidebar
- Accent color (`#ffd43b`) - consistent across all components
- Hover background (`rgba(255,255,255,0.05)`) - matches track-list
- Active background (`rgba(255,212,59,0.1)`) - matches track-list
- Danger hover (`#ff6b6b`) - standard for destructive actions

## Verification

After making changes, run:
```bash
cd frontend && pnpm exec tsc --noEmit
cd frontend && pnpm build
```
