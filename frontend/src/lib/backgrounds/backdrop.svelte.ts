// Which picture is behind the start page, and when it changes.
//
// The choice itself -- built-in pictures or a folder, one kept or all in turn,
// how long each stays -- is a preference and lives with the others in the
// settings file. What lives here is the rotation: where in the list it has got
// to, the seed and colour scheme the current scene was drawn with, and since
// when. That is kept
// in the browser's storage rather than the settings file because it is not a
// choice anybody made, and it changes on its own every quarter of an hour.
//
// The rotation only moves when it is looked at: the start page asks whether
// the picture is due for a change as it appears and every little while it is
// on screen. Nothing ticks while a pod list is open, and a start page seen
// again after lunch shows something new straight away.

// Read off the module rather than named in the import, as the session store
// does: the start page is part of the shell, so this is loaded under every
// test double of the services module, including ones written before this
// service existed. A named import such a module lacks fails as the module is
// linked; read at call time, it fails inside a `try` and reads as no folder.
import * as services from '../../../bindings/github.com/k8sdockside/k8sdockside/internal/services';
import type { BackgroundSettings } from '../state/adopt';
import { isDarkTheme, moodById, MOODS, moodPalette, paletteFor } from './palette';
import { renderScene, SCENES } from './scenes';

/** The mood a scene is drawn in when it takes the theme's own colours. */
export const THEME_MOOD = 'theme';

/**
 * One picture the start page can show. A scene carries the colour scheme it
 * is drawn in -- one of MOODS, or THEME_MOOD -- and whether that is by night
 * or by day is the theme's to say when it is drawn.
 */
export type Picture =
    | { id: string; kind: 'scene'; scene: string; seed: number; mood: string; name: string; blurb: string }
    | { id: string; kind: 'file'; url: string; name: string };

/** The name of a picture's colour scheme, for the caption; empty for the theme's own. */
export function moodName(picture: Picture): string {
    return picture.kind === 'scene' ? (moodById(picture.mood)?.name ?? '') : '';
}

/** What the settings view shows about the background folder. */
export interface BackgroundFolder {
    path: string;
    images: { name: string; url: string }[];
    problem: string;
}

/** Where the rotation has got to. */
interface Turn {
    index: number;
    seed: number;
    /** Which of MOODS the scene on screen is drawn in. */
    mood: number;
    /** When the picture on screen was put there, in ms since the epoch. */
    since: number;
}

const STORAGE_KEY = 'k8sdockside.backdrop';

/** How many drawn pictures are kept as object URLs before the oldest go. */
const CACHE_SIZE = 64;

/**
 * The seed a scene is drawn with when it is pinned or shown as a thumbnail.
 * Fixed per scene, so a pinned picture is the same picture tomorrow.
 */
export function stableSeed(id: string): number {
    let h = 2166136261;
    for (let i = 0; i < id.length; i++) {
        h ^= id.charCodeAt(i);
        h = Math.imul(h, 16777619);
    }
    return h >>> 0;
}

function freshSeed(): number {
    return Math.floor(Math.random() * 0xffffffff) >>> 0;
}

function readTurn(): Turn | null {
    try {
        const raw = localStorage.getItem(STORAGE_KEY);
        if (!raw) return null;
        const t = JSON.parse(raw) as Partial<Turn>;
        if (typeof t.index !== 'number' || typeof t.seed !== 'number' || typeof t.since !== 'number') return null;
        // Saved before pictures had moods: any one will do.
        const mood = typeof t.mood === 'number' ? t.mood : Math.floor(Math.random() * MOODS.length);
        return { index: t.index, seed: t.seed, mood, since: t.since };
    } catch {
        return null;
    }
}

function writeTurn(turn: Turn): void {
    try {
        localStorage.setItem(STORAGE_KEY, JSON.stringify(turn));
    } catch {
        // A private window, or storage switched off: the rotation simply
        // starts again next launch.
    }
}

/**
 * The built-in pictures, each at its stable seed and in a mood of its own:
 * taken in turn down the list, so no two neighbours in the gallery share one.
 */
export const BUILTIN_PICTURES: Picture[] = SCENES.map((scene, i) => ({
    id: `scene:${scene.id}`,
    kind: 'scene',
    scene: scene.id,
    seed: stableSeed(scene.id),
    mood: MOODS[i % MOODS.length].id,
    name: scene.name,
    blurb: scene.blurb,
}));

/** A different mood from the one given, at random. */
function anotherMood(current: number): number {
    if (MOODS.length < 2) return 0;
    return (current + 1 + Math.floor(Math.random() * (MOODS.length - 1))) % MOODS.length;
}

class Backdrop {
    folder = $state<BackgroundFolder>({ path: '', images: [], problem: '' });
    folderLoaded = $state(false);
    private turn = $state<Turn>(
        readTurn() ?? {
            index: Math.floor(Math.random() * SCENES.length),
            seed: freshSeed(),
            mood: Math.floor(Math.random() * MOODS.length),
            since: Date.now(),
        },
    );
    private cache = new Map<string, string>();

    /** Reads what is in the background folder. */
    async loadFolder(): Promise<void> {
        try {
            const got = await services.BackgroundService.Folder();
            this.adoptFolder(got);
        } catch {
            this.folder = { path: '', images: [], problem: '' };
        } finally {
            this.folderLoaded = true;
        }
    }

    /** Opens the native picker and reads the folder chosen. */
    async browseForFolder(): Promise<string> {
        try {
            this.adoptFolder(await services.BackgroundService.BrowseForFolder());
            return '';
        } catch (err) {
            return err instanceof Error ? err.message : String(err);
        }
    }

    async clearFolder(): Promise<void> {
        try {
            this.adoptFolder(await services.BackgroundService.ClearFolder());
        } catch {
            // Nothing to say: the folder shown is still the one in use.
        }
    }

    async revealFolder(): Promise<string> {
        try {
            await services.BackgroundService.RevealFolder();
            return '';
        } catch (err) {
            return err instanceof Error ? err.message : String(err);
        }
    }

    private adoptFolder(got: { path?: string; images?: { name: string; url: string }[] | null; problem?: string } | null): void {
        this.folder = {
            path: got?.path ?? '',
            images: [...(got?.images ?? [])],
            problem: got?.problem ?? '',
        };
    }

    /** The folder's images as pictures. */
    folderPictures = $derived<Picture[]>(
        this.folder.images.map((img) => ({ id: `file:${img.name}`, kind: 'file', url: img.url, name: img.name })),
    );

    /**
     * The pictures a setting draws from. A folder with nothing in it falls
     * back to the built-in ones, rather than to a blank start page nobody
     * asked for.
     */
    picturesFor(source: BackgroundSettings['source']): Picture[] {
        if (source === 'none') return [];
        if (source === 'folder' && this.folderPictures.length > 0) return this.folderPictures;
        return BUILTIN_PICTURES;
    }

    /**
     * A picture as the settings would have it drawn: in the theme's colours
     * when they say so, otherwise in the mood it carries.
     */
    styled(picture: Picture, settings: BackgroundSettings): Picture {
        return picture.kind === 'scene' && settings.palette === 'theme' ? { ...picture, mood: THEME_MOOD } : picture;
    }

    /** The picture to show now, for these settings. */
    current(settings: BackgroundSettings): Picture | null {
        const pictures = this.picturesFor(settings.source);
        if (pictures.length === 0) return null;
        const pinned = settings.pinned && pictures.find((p) => p.id === settings.pinned);
        if (pinned) return this.styled(pinned, settings);
        const at = ((this.turn.index % pictures.length) + pictures.length) % pictures.length;
        const picture = pictures[at];
        // A rotating scene is drawn afresh each time it comes round, in a
        // colour scheme it did not have last time.
        if (picture.kind !== 'scene') return picture;
        const mood = MOODS[((this.turn.mood % MOODS.length) + MOODS.length) % MOODS.length].id;
        return this.styled({ ...picture, seed: this.turn.seed, mood }, settings);
    }

    /** Moves on to the next picture now. */
    next(): void {
        this.turn = { index: this.turn.index + 1, seed: freshSeed(), mood: anotherMood(this.turn.mood), since: Date.now() };
        writeTurn(this.turn);
    }

    /** Moves on if the picture on screen has been there its full time. */
    advanceIfDue(minutes: number, now = Date.now()): boolean {
        if (now - this.turn.since < minutes * 60_000) return false;
        this.next();
        return true;
    }

    /**
     * A URL the picture can be drawn from. A scene is rendered in its mood --
     * by night or by day, as the theme given is dark or light -- or in the
     * theme's own colours, and kept as an object URL, so switching back and
     * forth between two of them does not draw either twice.
     */
    urlFor(picture: Picture, theme: { id: string; base: string; resolved: Record<string, string> } | null): string {
        if (picture.kind === 'file') return picture.url;
        const dark = isDarkTheme(theme);
        const own = picture.mood === THEME_MOOD ? null : (moodById(picture.mood) ?? MOODS[0]);
        const key = `${picture.scene}:${picture.seed}:${own ? `${own.id}:${dark ? 'night' : 'day'}` : `theme:${theme?.id ?? ''}`}`;
        const known = this.cache.get(key);
        if (known) return known;

        const palette = own ? moodPalette(own, dark) : paletteFor(theme?.resolved ?? {}, dark);
        const svg = renderScene(picture.scene, picture.seed, palette);
        const url =
            typeof URL !== 'undefined' && typeof URL.createObjectURL === 'function'
                ? URL.createObjectURL(new Blob([svg], { type: 'image/svg+xml' }))
                : `data:image/svg+xml;charset=utf-8,${encodeURIComponent(svg)}`;
        this.cache.set(key, url);
        if (this.cache.size > CACHE_SIZE) {
            const [oldest, stale] = this.cache.entries().next().value as [string, string];
            this.cache.delete(oldest);
            if (stale.startsWith('blob:')) URL.revokeObjectURL(stale);
        }
        return url;
    }
}

export const backdrop = new Backdrop();
