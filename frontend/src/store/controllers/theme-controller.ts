import type { ReactiveController, ReactiveControllerHost } from 'lit';
import type { ThemeState, BackgroundShade } from '../theme-store';
import { themeStore } from '../theme-store';

/**
 * ThemeController connects a Lit component to the ThemeStore.
 *
 * Most components do not need this because CSS custom properties
 * cascade into Shadow DOM automatically.  Use this controller only
 * in components that need to read or change theme values (e.g. the
 * config page colour picker).
 */
export class ThemeController implements ReactiveController {
    private host: ReactiveControllerHost;
    private unsubscribe?: () => void;

    constructor(host: ReactiveControllerHost) {
        this.host = host;
        host.addController(this);
    }

    // ===================================================================
    // LIFECYCLE HOOKS
    // ===================================================================

    hostConnected(): void {
        this.unsubscribe = themeStore.subscribe(() => {
            this.host.requestUpdate();
        });
    }

    hostDisconnected(): void {
        this.unsubscribe?.();
    }

    // ===================================================================
    // STATE ACCESSORS
    // ===================================================================

    get state(): Readonly<ThemeState> {
        return themeStore.getState();
    }

    get accentColor(): string {
        return this.state.accentColor;
    }

    get backgroundShade(): BackgroundShade {
        return this.state.backgroundShade;
    }

    // ===================================================================
    // ACTIONS
    // ===================================================================

    async setAccentColor(color: string): Promise<void> {
        await themeStore.setAccentColor(color);
    }

    async setBackgroundShade(
        shade: BackgroundShade,
    ): Promise<void> {
        await themeStore.setBackgroundShade(shade);
    }
}
