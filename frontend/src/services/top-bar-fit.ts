/**
 * What the top bar drops when it runs out of room (#143).
 *
 * The bar holds five children — the wordmark, back/forward, the library
 * filter, the search box and the job indicator — and at the bottom of
 * the Compact band they do not all fit. Measured on `main` at 600×600:
 * the bar is 611px inside a 600px viewport sitting still, and **862px
 * while a scan with a long title is running**, because `job-indicator`
 * is `hidden` when idle and up to 235px wide when it is not. `body` is
 * `overflow-x: auto`, so what a user sees is a horizontal scrollbar on
 * a shell that #24 promised would not need one.
 *
 * **The fit is measured, never breakpointed**, which is `page-header`'s
 * rule (#69) and applies here for a reason specific to this bar: three
 * of its five children are as wide as their *content*. The library
 * filter is a `<select>` sized by the longest library name, the job
 * indicator by the running job's title, and the search box by its
 * view-scoped placeholder — so any width you pick is right for exactly
 * one library, one job and one view. The same sweep that produced the
 * numbers above found the bar overflowing at every width from 600 to
 * 899 *and* at 900, where `nav-history` reappears; a breakpoint fixing
 * "600 to 610" would have fixed the case that happened to be idle.
 *
 * **What yields is chosen by #24's own sentence** — *no action is ever
 * unreachable at any supported size* — which rules out the two cheapest
 * candidates the issue lists. Hiding the library filter takes away an
 * action: `library-filter` is the **only** control in the app that sets
 * the selected library (nothing else calls `setSelectedLibrary`), so
 * hiding it is trading this promise for the same promise. Collapsing
 * the search box to an icon is what #57 wants on a phone, but #57 is
 * blocked behind #62 and building its modal here would be building it
 * without the thing that blocks it.
 *
 * So the two things that yield are the two that are **not** actions and
 * whose content survives elsewhere:
 *
 * 1. **The wordmark**, which is a brand — the window's own title bar
 *    says the same thing, and #48 wants it down to "YJ" at every width
 *    anyway. It yields its *width*, not its existence: the rule in
 *    `index.css` is visually-hidden rather than `display: none`, so the
 *    document keeps its top-level heading.
 * 2. **The job indicator's label**, leaving the ring. This is not a new
 *    judgement — the component already drops it below 600px for exactly
 *    this reason, and its `sr-only` live region is what announces the
 *    state either way, so nothing is lost to anyone. What a measurement
 *    adds is the band between 600 and 900, where whether the label fits
 *    depends on what else is in the bar rather than on the width alone.
 *
 * Measured against the running app with a long-titled scan staged, that
 * order fits at every width from 320 to 1440 — and collapses nothing at
 * 320, 390, 599, 899 and 1100, which is the other half of the claim.
 *
 * Three things about the mechanism are load-bearing.
 *
 * **Every pass starts from all-visible**, so the collapsed set is a
 * pure function of the current width rather than of how the window got
 * there. `page-header` states the same rule and the same reasons: a
 * pass that only ever added would never give the wordmark back, and one
 * that adjusted by a step would need a hysteresis band to stop it
 * oscillating on the pixel where it exactly fits.
 *
 * **"Fits" is the children against the content box, not `scrollWidth`
 * against `clientWidth`** — and that distinction is not pedantry, it
 * is a measured false pass. `scrollWidth` counts a box's *left*
 * padding and not its right, so with this bar's 2em gutters it
 * under-reports by 32px: at 700px with a scan running it read
 * `700/700`, a perfect fit, while `job-indicator` ended 32px past
 * where the content may go and sat in the gutter. Same family as #69's
 * title trap, one property over — the measurement that is easiest to
 * reach for is the one that cannot see the failure. So the predicate
 * here is the same one `top-bar-fit.spec.ts` asserts: no in-flow child
 * outside the content box.
 *
 * That is only truthful in turn because **nothing here absorbs pressure
 * by truncating**. The collapsible children are `flex-shrink: 0` in
 * `index.css`, so a deficit shows up as a child out of bounds instead
 * of quietly eating the indicator's label, which is `text-overflow:
 * ellipsis` and would have. The search box is the one child that may
 * shrink, between its 320px basis and the 200px floor its own
 * stylesheet sets, and a narrower input hides nothing it was showing.
 *
 * **The bar does not resize when a job starts**, which is the case the
 * whole thing is for. A ResizeObserver on the header alone never fires:
 * the indicator goes 0 → 235 inside a bar whose width has not changed.
 * Every element child is observed too.
 */

/** One thing the bar can give up, cheapest first. */
interface FitStep {
    /** For tests and for reading the DOM back. */
    readonly id: string;
    /** Applied to the bar; `on` collapses. */
    readonly collapse: (bar: HTMLElement, on: boolean) => void;
}

/**
 * The order things are given up in. Lowest priority first — see the
 * argument above for why these two and not the library filter.
 */
export const FIT_STEPS: readonly FitStep[] = [
    {
        id: 'wordmark',
        collapse: (bar, on) =>
            bar.querySelector('hgroup')?.classList.toggle('yj-collapsed', on),
    },
    {
        id: 'job-label',
        collapse: (bar, on) =>
            bar.querySelector('job-indicator')?.toggleAttribute('compact', on),
    },
];

/**
 * Decide what the bar shows at its current width.
 *
 * Exported for the component tier, which can hand it a bar of known
 * widths; the app installs the observer below and never calls this.
 *
 * @returns the ids collapsed, in the order they were given up.
 */
export function measureTopBarFit(bar: HTMLElement): string[] {
    const fits = () => {
        const style = getComputedStyle(bar);
        const box = bar.getBoundingClientRect();
        const left = box.left + parseFloat(style.paddingLeft);
        const right = box.right - parseFloat(style.paddingRight);

        for (const child of bar.children) {
            const cs = getComputedStyle(child);

            // Out of flow is out of the question: a collapsed wordmark
            // is absolutely positioned and 1px wide precisely so that
            // it costs the row nothing.
            if (cs.display === 'none' || cs.position === 'absolute') continue;

            const r = child.getBoundingClientRect();

            // Sub-pixel slack: a flex row's widths are fractional and a
            // rounding difference is not an overflow anyone can see.
            if (r.width > 0 && (r.right > right + 0.5 || r.left < left - 0.5)) {
                return false;
            }
        }

        return true;
    };

    for (const step of FIT_STEPS) step.collapse(bar, false);

    const collapsed: string[] = [];

    if (!fits()) {
        for (const step of FIT_STEPS) {
            step.collapse(bar, true);
            collapsed.push(step.id);

            if (fits()) break;
        }
    }

    return collapsed;
}

/**
 * Watch the bar and its children, and keep it fitting.
 *
 * Returns the uninstaller, which the tests use; the app installs once
 * for the life of the session.
 */
export function installTopBarFit(bar: HTMLElement): () => void {
    let measuring = false;

    const measure = () => {
        // A pass resizes the children it collapses, which the observer
        // would report back to us. It settles either way — the pass is
        // idempotent at a given width — but re-entering it is work for
        // no news, and it is what "ResizeObserver loop completed with
        // undelivered notifications" is.
        if (measuring) return;

        measuring = true;

        try {
            measureTopBarFit(bar);
        } finally {
            measuring = false;
        }
    };

    const observer = new ResizeObserver(measure);

    observer.observe(bar);

    for (const child of bar.children) observer.observe(child);

    measure();

    return () => observer.disconnect();
}
