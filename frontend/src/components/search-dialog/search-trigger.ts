/**
 * The phone's way into search (#57): one button, in the row that
 * already says which page you are on.
 *
 * **Which views show it is not a decision this component makes.**
 * `store/search-store.ts` has held the map of what each view searches
 * since plan 007, and #57's own Findings say so — "that is exactly the
 * condition for showing the button". So this asks `isSearchableView`
 * and renders nothing otherwise, and no second list of searchable views
 * exists to fall out of step with the first.
 *
 * **It is an element rather than a `PageAction`**, and that is the
 * whole reason it is a component at all. Two of the seven searchable
 * views — `playlist-details` and `smart-playlist-details` — have no
 * `page-header`; they filter on the term and say so in their own
 * headers. Declaring search as an action would mean seven hosts each
 * writing it out, which is the second list again, and it would put a
 * *phone mode for actions* inside `page-header`, which that component
 * documents its refusal to grow. An element three headers place is one
 * statement of the rule, placed three times.
 *
 * It does not participate in `page-header`'s overflow measurement, for
 * the reason the count and the sort control do not: it is 32px, it is
 * `flex-shrink: 0`, and the header's `fits()` sees its width like any
 * other child. What it must never do is collapse into the overflow
 * menu — on a phone that menu is the only home for the page's actions
 * already, and search would be two taps behind an ellipsis.
 */
import { LitElement, css, html, nothing } from 'lit';
import { customElement, state } from 'lit/decorators.js';
import '@awesome.me/webawesome/dist/components/icon/icon.js';

import { designTokens } from '../../styles/tokens.css';
import { PHONE_QUERY } from '@utils/breakpoints';
import { SearchController } from '@store/controllers/search-controller';
import { ICON_SEARCH } from '@utils/icon-language';
import { OPEN_SEARCH_EVENT } from './search-dialog';

@customElement('search-trigger')
export class SearchTrigger extends LitElement {
    private searchCtrl = new SearchController(this);

    /**
     * From `matchMedia` rather than a media query, because this decides
     * whether the button *exists* — `job-band`'s rule, and for the same
     * consequence: a header that renders it at every width puts a
     * second search affordance beside the desktop's own box.
     */
    @state() private phone = false;

    private media?: MediaQueryList;

    static override styles = [
        designTokens,
        css`
            :host {
                display: contents;
            }

            button {
                display: inline-flex;
                align-items: center;
                justify-content: center;
                /* The app's touch floor, from #56 -- and this is the
                   control that should least have to argue for it: #57
                   created it as the phone's replacement for the header
                   search box, so it exists *only* where there is a
                   thumb.

                   It shipped at 40px under a comment calling that "the
                   smallest a touch target should be", which was the
                   floor being restated four pixels short rather than a
                   second opinion about it (#186). The rest of that
                   comment said the header's own action buttons are
                   smaller because they carry a label; they are 44px
                   now too, so that no longer distinguishes anything.

                   The extra width is a target rather than a box, for
                   page-header's reason: this button sits in that
                   header, whose overflow fit (#69) measures inline
                   size, and four pixels there is four pixels the
                   trigger for every collapsed action does not get at
                   320px. Height is free -- nothing measures it. */
                min-width: 44px;
                min-height: 44px;
                /* Border-box, so the 44 above is the whole target and
                   the margin is what hands the four extra pixels back
                   to the row. */
                margin-inline: -2px;
                padding: 0;
                background: none;
                border: 1px solid var(--yj-border-subtle, #555);
                border-radius: 4px;
                color: var(--yj-text-primary, #fff);
                cursor: pointer;
                flex-shrink: 0;
            }

            button:focus-visible {
                outline: 2px solid var(--yj-accent, #ffd43b);
                outline-offset: -1px;
            }

            /* A search that is *on* says so without a second control:
               the page already carries "Showing albums matching ...",
               and this is the button that reopens the box to change or
               clear it. */
            button.filtering {
                border-color: var(--yj-accent, #ffd43b);
                color: var(--yj-accent-text, #ffd43b);
            }
        `,
    ];

    override connectedCallback(): void {
        super.connectedCallback();

        this.media = window.matchMedia(PHONE_QUERY);
        this.phone = this.media.matches;
        this.media.addEventListener('change', this.onMedia);
    }

    override disconnectedCallback(): void {
        super.disconnectedCallback();
        this.media?.removeEventListener('change', this.onMedia);
    }

    private onMedia = (e: MediaQueryListEvent): void => {
        this.phone = e.matches;
    };

    private onClick = (): void => {
        document.dispatchEvent(new CustomEvent(OPEN_SEARCH_EVENT));
    };

    override render() {
        if (!this.phone || !this.searchCtrl.isSearchableView) return nothing;

        const scope = this.searchCtrl.scopeLabel;
        const term = this.searchCtrl.term;

        // The name carries the state, because the colour cannot: a
        // control that is a different colour and the same word is a
        // control that says nothing to anyone not seeing it. Same rule
        // `library-status.ts` states for a partial badge.
        const label = term
            ? `Search ${scope}, showing matches for ${term}`
            : `Search ${scope}`;

        return html`
            <button
                data-testid="search-trigger"
                class=${term ? 'filtering' : ''}
                aria-label=${label}
                title=${label}
                @click=${this.onClick}
            >
                <wa-icon name=${ICON_SEARCH}></wa-icon>
            </button>
        `;
    }
}

declare global {
    interface HTMLElementTagNameMap {
        'search-trigger': SearchTrigger;
    }
}
