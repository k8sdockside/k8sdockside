// The settings profiles: which one this window runs on, which others there
// are, and starting the app on one of them.
//
// A profile is chosen when the app starts, so "switching" is the backend
// starting a new copy on the other profile and this one quitting; "alongside"
// is the new copy without the quitting. See internal/services/profiles.go.

import { SettingsService } from '../../../bindings/github.com/k8sdockside/k8sdockside/internal/services';
import { notices } from './notices.svelte';
import { session } from './session.svelte';

/** The name a profile goes by in the interface: the default has none of its own. */
export function profileLabel(name: string): string {
    return name || 'default';
}

/** What a new profile may be called, as the backend checks it. */
export const PROFILE_NAME = /^[a-z0-9][a-z0-9_-]{0,31}$/;

class ProfilesStore {
    current = $state('');
    /** Every profile there is, the default first as ''. */
    all = $state<string[]>(['']);
    loaded = $state(false);
    /** The profile being started, while it is. */
    starting = $state<string | null>(null);

    async load(): Promise<void> {
        if (session.server) return;
        try {
            const got = await SettingsService.Profiles();
            this.current = got.current ?? '';
            this.all = ['', ...(got.profiles ?? [])];
            this.loaded = true;
        } catch {
            // The switcher is a convenience; without the list it offers the
            // default and this window's own, which it knows anyway.
        }
    }

    /** Relaunches the app on another profile. This window closes once the new one is up. */
    switchTo(name: string): Promise<void> {
        return this.open(name, true);
    }

    /** Starts another copy of the app on a profile, beside this one. */
    openAlongside(name: string): Promise<void> {
        return this.open(name, false);
    }

    private async open(name: string, replace: boolean): Promise<void> {
        this.starting = name;
        try {
            await SettingsService.OpenProfile(name, replace);
            notices.inform(
                replace
                    ? `Switching to the ${profileLabel(name)} profile…`
                    : `Opened K8s Dockside on the ${profileLabel(name)} profile`,
            );
            if (!this.all.includes(name)) this.all = [...this.all, name];
        } catch (err) {
            notices.fail(err instanceof Error ? err.message : String(err));
        } finally {
            this.starting = null;
        }
    }
}

export const profiles = new ProfilesStore();
