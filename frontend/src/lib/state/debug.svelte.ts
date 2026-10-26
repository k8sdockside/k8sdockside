// What `kubectl debug` from the app needs on this side: the preset images, read
// once from the backend, and the request the launch form builds.
//
// The presets live in Go (appconfig.DebugPresets) because the settings store
// needs them too -- an added image that is already a preset is dropped there --
// so this only asks for them, and copes with not being told.

import { TerminalService } from '../../../bindings/github.com/k8sdockside/k8sdockside/internal/services';
import type * as kube from '../../../bindings/github.com/k8sdockside/k8sdockside/internal/kube/models.js';

/** One image offered without having been added. */
export interface DebugPreset {
    image: string;
    label: string;
    hint: string;
}

/** The ways in, as kubectl debug has them. */
export type DebugMode = 'ephemeral' | 'copy' | 'node';

/**
 * The profiles kubectl debug knows. general is kubectl's default; legacy is
 * deprecated there and kept here only because kubectl still accepts it.
 */
export const DEBUG_PROFILES: { value: string; label: string; hint: string }[] = [
    { value: 'general', label: 'general', hint: 'The default: adds SYS_PTRACE so a debugger can attach.' },
    { value: 'baseline', label: 'baseline', hint: 'Nothing beyond what the baseline pod security standard allows.' },
    { value: 'restricted', label: 'restricted', hint: 'Fits the restricted standard: non-root, no capabilities.' },
    { value: 'netadmin', label: 'netadmin', hint: 'Adds NET_ADMIN and NET_RAW, for tcpdump, iptables and friends.' },
    { value: 'sysadmin', label: 'sysadmin', hint: 'Privileged. For a namespace that allows it, and nothing else.' },
    { value: 'legacy', label: 'legacy (deprecated)', hint: 'What kubectl did before profiles. Deprecated in kubectl.' },
];

/** The image pull policies, with empty meaning "the cluster's default". */
export const PULL_POLICIES = ['', 'IfNotPresent', 'Always', 'Never'];

/** What a debug session is asked for: kube.DebugRequest, with every field present. */
export type DebugRequest = kube.DebugRequest;

/**
 * A request with nothing chosen yet but where it goes, holding kubectl's own
 * defaults: interactive, processes shared in a copy, init containers kept,
 * and labels, annotations and probes left behind.
 */
export function blankDebugRequest(mode: DebugMode, namespace: string, name: string): DebugRequest {
    return {
        mode,
        namespace,
        name,
        container: '',
        image: '',
        imagePullPolicy: '',
        env: [],
        command: [],
        argumentsOnly: false,
        profile: '',
        custom: '',
        interactive: true,
        target: '',
        copyTo: mode === 'copy' ? `${name}-debug` : '',
        setImages: [],
        shareProcesses: true,
        sameNode: false,
        replace: false,
        keepLabels: false,
        keepAnnotations: false,
        keepLiveness: false,
        keepReadiness: false,
        keepStartup: false,
        keepInitContainers: true,
        keep: false,
    };
}

/**
 * A list typed into one field -- KEY=VALUE pairs, name=image pairs -- split on
 * commas or new lines, with the empty pieces dropped. Not on spaces: a value
 * may hold one.
 */
export function splitList(text: string): string[] {
    return text
        .split(/[,\n]+/)
        .map((item) => item.trim())
        .filter((item) => item !== '');
}

/**
 * A command as typed, split into its words. Quotes group a word with spaces in
 * it, which is as much shell as a one-line field needs: anything more is a job
 * for `sh -c '…'`, which this splits correctly.
 */
export function splitCommand(text: string): string[] {
    const words: string[] = [];
    let current = '';
    let quote: string | null = null;
    let started = false;
    for (const ch of text) {
        if (quote) {
            if (ch === quote) quote = null;
            else current += ch;
            continue;
        }
        if (ch === '"' || ch === "'") {
            quote = ch;
            started = true;
        } else if (/\s/.test(ch)) {
            if (started) words.push(current);
            current = '';
            started = false;
        } else {
            current += ch;
            started = true;
        }
    }
    if (started) words.push(current);
    return words;
}

class DebugPresets {
    list = $state<DebugPreset[]>([]);
    private asked = false;

    /** Reads the presets the first time anything wants them. */
    async load(): Promise<void> {
        if (this.asked) return;
        this.asked = true;
        try {
            this.list = ((await TerminalService.DebugPresets()) ?? []).map((p) => ({
                image: p.image,
                label: p.label,
                hint: p.hint,
            }));
        } catch {
            // Without the presets the picker still offers the user's own
            // images and the default, and typing one in still works. Asked
            // again next time.
            this.asked = false;
        }
    }

    /** The preset images, by image. */
    images(): string[] {
        return this.list.map((p) => p.image);
    }
}

export const debugPresets = new DebugPresets();

/**
 * Every image the picker offers: the presets, then the user's own, then the
 * default if it is neither -- an image made the default from the form need not
 * be in either list.
 */
export function debugImageChoices(presets: DebugPreset[], custom: string[], current: string): { image: string; label: string }[] {
    const out = presets.map((p) => ({ image: p.image, label: `${p.label} — ${p.image}` }));
    for (const image of custom) {
        if (!out.some((o) => o.image === image)) out.push({ image, label: image });
    }
    if (current && !out.some((o) => o.image === current)) out.push({ image: current, label: current });
    return out;
}
