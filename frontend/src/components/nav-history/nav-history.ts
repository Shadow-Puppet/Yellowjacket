import { LitElement, html, css } from 'lit';
import { customElement } from 'lit/decorators.js';
import '@awesome.me/webawesome/dist/components/icon/icon.js';
import { designTokens } from '../../styles/tokens.css';
import { HistoryController } from '@store/controllers/history-controller';

/**
 * Global back and forward, in the top bar (#6).
 *
 * **The stack was already global; the affordance was not.** Every
 * navigation has been a history entry since the Android back gesture
 * landed, and `popstate` restores any of them in either direction --
 * `back-navigation.spec.ts` has asserted `goForward()` since it was
 * written. What the report describes as "back is tab-scoped" is that
 * the *only* way back was a detail view's own button, which vanishes
 * the moment you leave for another tab: the album you were reading is
 * still one entry away, and nothing on screen says so or offers it.
 *
 * Four things about this are load-bearing.
 *
 * **It asks the shell rather than the History API.** `history.length`
 * counts entries this app did not push and never shrinks, and there is
 * no way to ask where in the list you are -- so a control derived from
 * it is confidently wrong at both ends. `historyStore` is the shell's
 * own numbering.
 *
 * **A control that cannot act is `disabled`, not hidden.** This is the
 * one place in the app where that is right rather than the fault
 * `library-status-indicator` was: back and forward are a *pair* whose
 * positions the user learns, and a button that disappears at the end
 * of the list moves the other one under the cursor. It is also what
 * every browser does, which is the whole design brief here.
 *
 * **The buttons dispatch the events the rest of the app already
 * dispatches**, `navigate-back` and `navigate-forward`, rather than
 * calling `history.back()` themselves. The shell owns the guard -- one
 * press is one entry, and at the root there is nothing of ours to go
 * back to -- and a second caller reaching for `history` directly is
 * how the old `navStack` came to disagree with the platform.
 *
 * **It is desktop chrome.** Below 600px the phone has a system back
 * gesture (and, on Android, a hardware/gesture Back that this app
 * hooks), the top bar is 3.25em with three other things in it, and two
 * more 32px targets there would be the first thing to overflow. Hidden
 * by `index.css` at that width, next to the rest of the phone header's
 * concessions.
 */
@customElement('nav-history')
export class NavHistory extends LitElement {
    private historyCtrl = new HistoryController(this);

    static override styles = [designTokens, css`
        :host {
            display: flex;
            align-items: center;
            gap: 0.25em;
            /* A grid item's implicit minimum is its content; this one
               genuinely cannot shrink, so it says so rather than
               letting the header widen the body. */
            flex: 0 0 auto;
        }

        button {
            display: flex;
            align-items: center;
            justify-content: center;
            width: 2em;
            height: 2em;
            padding: 0;
            border: none;
            border-radius: 50%;
            background: transparent;
            color: var(--yj-text-primary, #f8f9fa);
            cursor: pointer;
            font-size: 1em;
        }

        button:hover:not(:disabled) {
            background-color: var(--yj-bg-overlay, #495057);
        }

        button:focus-visible {
            outline: 2px solid var(--yj-accent, #ffd43b);
            outline-offset: 2px;
        }

        button:disabled {
            /* Not a contrast failure: a disabled control is exempt from
               1.4.3, and the pair has to read as unavailable rather
               than merely quiet. */
            color: var(--yj-text-tertiary, #868e96);
            cursor: default;
        }
    `];

    private go(direction: 'back' | 'forward') {
        this.dispatchEvent(new CustomEvent(`navigate-${direction}`, {
            bubbles: true,
            composed: true,
        }));
    }

    override render() {
        const { canBack, canForward } = this.historyCtrl.depth;

        return html`
            <button
                type="button"
                data-testid="history-back"
                aria-label="Back"
                ?disabled=${!canBack}
                @click=${() => this.go('back')}
            >
                <wa-icon name="arrow-left"></wa-icon>
            </button>
            <button
                type="button"
                data-testid="history-forward"
                aria-label="Forward"
                ?disabled=${!canForward}
                @click=${() => this.go('forward')}
            >
                <wa-icon name="arrow-right"></wa-icon>
            </button>
        `;
    }
}

declare global {
    interface HTMLElementTagNameMap {
        'nav-history': NavHistory;
    }
}
