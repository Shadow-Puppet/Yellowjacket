/**
 * Every `wa-dialog` in this app is an unnamed dialog. This names them.
 *
 * `a11y.md` lists all of them under "what is already correct" and notes
 * that every call site passes a `label` — both true, and the label never
 * reaches the accessibility tree. Web Awesome renders it into an
 * `<h2 part="title" id="title">` in the *same shadow root* as the native
 * `<dialog part="dialog">` and never points `aria-labelledby` at it, so
 * `getByRole('dialog', {name})` matches nothing and a screen reader
 * announces an unnamed dialog. Confirmed in the installed source
 * (`dist/chunks/chunk.ZUIYLL2X.js`, `WaDialog.render`).
 *
 * Three decisions are worth stating, because each has a failure mode:
 *
 * **This reaches into another library's shadow root**, which is open and
 * therefore reachable, but is not API. It is acceptable here precisely
 * because the failure is bounded: if Web Awesome changes the structure,
 * the query returns null, the helper does nothing, and the dialog is as
 * unnamed as it is today. There is no state to get wrong and nothing to
 * throw. The alternative — patching `WaDialog.prototype` — fixes every
 * call site for free and fails loudly and strangely instead.
 *
 * **It prefers `aria-labelledby` over `aria-label`.** Three call sites
 * have a label that changes after the first render (`track-details`,
 * `confirm-dialog`, `notification-host`), and an IDREF to the `<h2>` the
 * dialog re-renders anyway stays correct for free. `aria-label` is the
 * fallback for `without-header`, which renders no `<h2>` at all
 * (`first-run-wizard` is the one such caller).
 *
 * **And it waits for the dialog's own first update, not its host's.**
 * `wa-dialog` is a Lit element: its shadow root is populated in *its*
 * update, which has not run when the host reaches `firstUpdated`. A
 * synchronous query there finds an element with an empty shadow root
 * and names nothing — the same trap that made `wa-dropdown-item`'s role
 * invisible to the menu keyboard model two passes ago.
 */

/** The id Web Awesome gives its title heading, within its own shadow root. */
const WA_TITLE_ID = 'title';

/** A `wa-dialog` element, as much of it as this file needs. */
type DialogHost = Element & {
    label?: string;
    updateComplete?: Promise<unknown>;
};

/**
 * Dialogs already pointed at their own heading.
 *
 * Hosts call this from `updated()`, which runs on every pass, so the
 * steady-state cost has to be a lookup rather than an attribute write.
 * Only the IDREF path is cached: it cannot go stale, because the `<h2>`
 * it names re-renders with the label. The `aria-label` fallback is
 * re-applied every call, since a copied string can.
 */
const named = new WeakSet<Element>();

/** Set the name, if the shadow root is there to set it on. Returns success. */
function applyName(host: DialogHost): boolean {
    const root = host.shadowRoot;

    if (!root) return false;

    const dialog = root.querySelector('dialog[part~="dialog"]');

    if (!dialog) return false;

    // The header, and so the heading the name comes from, is absent
    // under `without-header`.
    if (root.querySelector(`#${WA_TITLE_ID}`)) {
        dialog.setAttribute('aria-labelledby', WA_TITLE_ID);
        dialog.removeAttribute('aria-label');
        named.add(host);

        return true;
    }

    const label = host.label ?? host.getAttribute('label') ?? '';

    if (label) {
        dialog.setAttribute('aria-label', label);
        dialog.removeAttribute('aria-labelledby');
    }

    return true;
}

/**
 * Point the native `<dialog>` inside a `<wa-dialog>` at its own title.
 *
 * Safe to call before the dialog has rendered, and safe to call more
 * than once — it is a no-op when there is nothing to name. Call it from
 * the host's `firstUpdated()`, or from `updated()` for a host that
 * renders its dialog conditionally.
 */
export function nameDialog(host: DialogHost | null | undefined): void {
    if (!host || named.has(host)) return;

    if (applyName(host)) return;

    // Not rendered yet: wait for the dialog's own update, then retry
    // once. Anything still missing after that is a structure this
    // helper does not recognise, which is the bounded failure above.
    void host.updateComplete?.then(() => {
        applyName(host);
    });
}

/**
 * Name every `wa-dialog` rendered in `root`.
 *
 * The hosts that render two dialogs, or render one conditionally, use
 * this rather than tracking a `@query` each.
 */
export function nameDialogsIn(root: ShadowRoot | null | undefined): void {
    if (!root) return;

    for (const host of root.querySelectorAll('wa-dialog')) {
        nameDialog(host as DialogHost);
    }
}
