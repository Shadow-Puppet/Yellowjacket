/**
 * The ambient shortcut scope: which panel's bindings apply when nothing
 * inside a panel has focus.
 *
 * `resolveScope` in the shortcut service walks up from the focused
 * element looking for `data-shortcut-scope`, which is the right answer
 * when something is focused — but this app is mostly driven with the
 * mouse, so the usual state is that focus sits on `<body>` and the walk
 * finds nothing.  Without a fallback the panel bindings would only work
 * after a click landed inside the panel, which is not the behaviour the
 * Autotag page has today and not one worth regressing to.
 *
 * A claim is held by the *active view* (see `utils/view-lifecycle.ts`),
 * so it is released the moment the view leaves the screen — which is
 * what stops an off-screen view's keys from firing.
 */

/** Claims, innermost last.  A stack rather than a single value so that
 *  releasing an outer claim out of order cannot resurrect it. */
const claims: Array<{ scope: string }> = [];

/**
 * Claim `scope` as the ambient panel scope.  Returns the release.
 */
export function claimShortcutScope(scope: string): () => void {
    const claim = { scope };

    claims.push(claim);

    return () => {
        const at = claims.indexOf(claim);

        if (at >= 0) claims.splice(at, 1);
    };
}

/** The innermost claimed scope, or null. */
export function ambientShortcutScope(): string | null {
    return claims.length > 0 ? claims[claims.length - 1]!.scope : null;
}

/** Drop every claim.  Test-only escape hatch. */
export function resetShortcutScopes(): void {
    claims.length = 0;
}
