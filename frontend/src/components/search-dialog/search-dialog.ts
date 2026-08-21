/**
 * The phone's search surface (#57).
 *
 * Below 600px there is no top bar to hold a search box — the bar is out
 * of the layout entirely, which is the single biggest vertical win
 * available on a 439 CSS px viewport. So the box moves into a modal and
 * the *trigger* moves into the row that already says which page you are
 * on (`search-trigger`, beside this file).
 *
 * **It is a `wa-dialog`, and that is a mechanism rather than a taste.**
 * #60 read this out of the Web Awesome source: `wa-popup` renders
 * `<div popover="manual">` and feature-detects the Popover API, falling
 * back to `strategy: "fixed"` where there is none — which is the
 * reference device, Chrome 113, since `popover` is Chrome 114. And
 * `position: fixed` escapes ancestor *overflow* but not `contain:
 * paint`, which makes an element a containing block for fixed
 * descendants **and clips them**; `index.css` puts `contain: layout
 * style paint` on `.main-panel`, which is the ancestor of every view.
 * A popup-shaped search panel opened from a view's header would
 * therefore be structurally clipped on the one device this issue is
 * about, and **no tier here could see it** — CI's Chromium and WebKit
 * both have the Popover API, so the popup is top-layered and correct.
 * `<dialog>`/`showModal()` is Chrome 37 and uses the real top layer, so
 * this is immune by construction.
 *
 * **It carries the real `<search-bar>`**, not a second input. That is
 * what keeps one debounce, one clear button, one accessible name and
 * one view-scoped placeholder — and it is why `store/search-store.ts`
 * is still the only statement of which views can search and what they
 * search. The modal is a presentation of the control, not a copy of it.
 *
 * **The results are the view, not a list in here.** The Direction says
 * "the box and live results"; the live results already exist, because
 * the term is view-scoped and the page behind this dialog filters on it
 * and says so in `page-header`'s "Showing albums matching …" line.
 * Rendering results in the dialog would be a second implementation of
 * every view's own filtering, and a worse one — it could not offer the
 * row actions the view does. So Enter closes and hands the screen back.
 *
 * A singleton in `index.html` for the reason `shortcuts-overlay` is:
 * one instance, one `data-testid`, one document listener, and no
 * `data-testid="search-input"` resolving to two elements while it is
 * shut.
 */
import { LitElement, css, html, nothing } from 'lit';
import { customElement, query, state } from 'lit/decorators.js';
import '@awesome.me/webawesome/dist/components/dialog/dialog.js';

import { designTokens } from '../../styles/tokens.css';
import { nameDialogsIn } from '@utils/name-dialog';
import { SearchController } from '@store/controllers/search-controller';
import type { SearchBar } from '../search-bar/search-bar';
import '../search-bar/search-bar';

/** The event any trigger dispatches to open this. */
export const OPEN_SEARCH_EVENT = 'open-search';

@customElement('search-dialog')
export class SearchDialog extends LitElement {
    private searchCtrl = new SearchController(this);

    @query('wa-dialog') private dialog?: HTMLElement & { open: boolean };

    @query('search-bar') private bar?: SearchBar;

    @state() private isOpen = false;

    static override styles = [
        designTokens,
        css`
            :host {
                display: contents;
            }

            wa-dialog::part(dialog) {
                background: var(--yj-bg-surface, #212529);
                color: var(--yj-text-primary, #fff);
            }

            /* The box is the whole content, so it gets the whole width
               rather than the 360px cap it wears in a header. */
            search-bar {
                display: block;
                width: 100%;
                --yj-search-max-width: none;
            }

            .hint {
                margin: 0.75em 0 0;
                font-size: var(--yj-text-sm, 0.8125rem);
                color: var(--yj-text-secondary, #b3b3b3);
            }
        `,
    ];

    override connectedCallback(): void {
        super.connectedCallback();
        document.addEventListener(OPEN_SEARCH_EVENT, this.open);
        // Capture, on the host: the path runs document -> host ->
        // shadow root -> the input inside `search-bar`, so a capture
        // listener here is the only one that gets the key *before* the
        // input's own handler. A `@keydown` in the template is a
        // bubbling listener and would run after the term was cleared,
        // and there is nowhere to put a `firstUpdated` hook -- the
        // first render of this element produces no content at all.
        this.addEventListener('keydown', this.onKeydown, true);
    }

    override disconnectedCallback(): void {
        super.disconnectedCallback();
        document.removeEventListener(OPEN_SEARCH_EVENT, this.open);
        this.removeEventListener('keydown', this.onKeydown, true);
    }

    /**
     * Not a toggle, for `shortcuts-overlay`'s reason: a dialog owns
     * every unmodified key while it is up, so a second press of the
     * shortcut that opened it never reaches the shortcut service.
     */
    private open = (): void => {
        if (this.isOpen) return;

        // Nothing to search here is not an error; it is the state the
        // trigger already declines to render in. Guarding here too is
        // what makes the keyboard route (Ctrl+F on a phone) agree with
        // the button.
        if (!this.searchCtrl.isSearchableView) return;

        this.isOpen = true;

        void this.updateComplete.then(() => {
            if (this.dialog) this.dialog.open = true;

            // `wa-dialog` positions and shows in its own update, and
            // `search-bar` populates its own shadow root in one more —
            // the same lifecycle trap `name-dialog.ts` documents. One
            // more frame, and the box has an input to focus.
            requestAnimationFrame(() => this.bar?.focusInput());
        });
    };

    private close(): void {
        if (this.dialog) this.dialog.open = false;

        this.isOpen = false;
    }

    /**
     * Escape closes and **keeps the term**; Enter closes and shows the
     * results.
     *
     * Escape is the one worth stating. `search-bar`'s input treats it
     * as *clear the search*, which is right in a header — the box is on
     * screen either way, so clearing is the only thing left for the key
     * to mean. Here it would make dismissing the search surface
     * silently discard the search, and discarding is what the clear
     * button inside it is for. So this runs first and closes; the term
     * survives, and the page behind is still filtered by it.
     */
    private onKeydown = (e: KeyboardEvent): void => {
        if (!this.isOpen) return;

        if (e.key === 'Escape') {
            e.stopPropagation();
            this.close();

            return;
        }

        if (e.key === 'Enter') {
            e.stopPropagation();
            e.preventDefault();
            this.close();
        }
    };

    /**
     * Web Awesome renders `label` into a heading it never points the
     * `<dialog>` at. See `utils/name-dialog.ts`.
     */
    override updated(): void {
        nameDialogsIn(this.shadowRoot);
    }

    override render() {
        if (!this.isOpen) return nothing;

        const scope = this.searchCtrl.scopeLabel;

        return html`
            <wa-dialog
                label=${`Search ${scope}`}
                data-testid="search-dialog"
                @wa-hide=${() => this.close()}
            >
                <search-bar></search-bar>
                <p class="hint">
                    Results appear on the page behind this. Press Enter
                    or close to see them.
                </p>
            </wa-dialog>
        `;
    }
}

declare global {
    interface HTMLElementTagNameMap {
        'search-dialog': SearchDialog;
    }
}
