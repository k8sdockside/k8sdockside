<!--
  A single-stroke icon set, kept inline so the app has no icon dependency and
  every glyph inherits currentColor. Each icon is a list of path commands drawn
  on a 24x24 grid.
-->
<script lang="ts" module>
    /**
     * Exported so the catalogue can be checked against it: an icon name with no
     * path here renders an empty svg, which lays out normally and so hides the
     * typo until someone looks closely.
     */
    export const PATHS: Record<string, string[]> = {
        // Resource kinds
        dashboard: ['M4 4h6v6H4z', 'M14 4h6v6h-6z', 'M4 14h6v6H4z', 'M14 14h6v6h-6z'],
        server: ['M4 5h16v5H4z', 'M4 14h16v5H4z', 'M7.5 7.5h.01', 'M7.5 16.5h.01'],
        layers: ['M12 3 3 8l9 5 9-5-9-5z', 'M3 13l9 5 9-5'],
        bell: ['M18 9a6 6 0 1 0-12 0c0 5-2 6-2 6h16s-2-1-2-6', 'M10.3 20a2 2 0 0 0 3.4 0'],
        box: ['M20 8 12 4 4 8v8l8 4 8-4V8z', 'M4 8l8 4 8-4', 'M12 12v8'],
        rocket: ['M3 7l9-4 9 4-9 4-9-4z', 'M3 12l9 4 9-4', 'M3 17l9 4 9-4'],
        database: [
            'M12 3c4.4 0 8 1.3 8 3s-3.6 3-8 3-8-1.3-8-3 3.6-3 8-3z',
            'M4 6v12c0 1.7 3.6 3 8 3s8-1.3 8-3V6',
            'M4 12c0 1.7 3.6 3 8 3s8-1.3 8-3',
        ],
        repeat: ['M17 2l4 4-4 4', 'M3 11V9a4 4 0 0 1 4-4h14', 'M7 22l-4-4 4-4', 'M21 13v2a4 4 0 0 1-4 4H3'],
        check: ['M9 11.5l2.5 2.5L20 5.5', 'M20 12v7a2 2 0 0 1-2 2H6a2 2 0 0 1-2-2V6a2 2 0 0 1 2-2h9'],
        clock: ['M12 3a9 9 0 1 0 0 18 9 9 0 0 0 0-18z', 'M12 7v5l3 2'],
        share: [
            'M18 8a3 3 0 1 0 0-6 3 3 0 0 0 0 6z',
            'M6 15a3 3 0 1 0 0-6 3 3 0 0 0 0 6z',
            'M18 22a3 3 0 1 0 0-6 3 3 0 0 0 0 6z',
            'M8.6 13.4l6.8 3.9',
            'M15.4 6.7l-6.8 3.9',
        ],
        globe: ['M12 3a9 9 0 1 0 0 18 9 9 0 0 0 0-18z', 'M3 12h18', 'M12 3a14 14 0 0 1 0 18 14 14 0 0 1 0-18z'],
        sliders: ['M4 7h10', 'M18 7h2', 'M4 17h4', 'M12 17h8', 'M16 5v4', 'M8 15v4'],
        lock: ['M6 10h12v10H6z', 'M9 10V7a3 3 0 0 1 6 0v3'],
        // The same padlock with the shackle swung open, so the pair reads as
        // one control in two states rather than as two different icons.
        unlock: ['M6 10h12v10H6z', 'M9 10V7a3 3 0 0 1 6 0'],
        // Media-style controls, for plugins whose objects start and stop:
        // a virtual machine, a suspended job, a paused rollout.
        play: ['M7 4.5v15l12-7.5z'],
        pause: ['M7 5h3.5v14H7z', 'M13.5 5H17v14h-3.5z'],
        stop: ['M6 6h12v12H6z'],
        power: ['M12 3v8', 'M6.3 6.3a8 8 0 1 0 11.4 0'],
        drive: ['M5 12l2-6h10l2 6v6H5v-6z', 'M5 12h14', 'M8 15.5h.01', 'M11 15.5h.01'],
        // A gateway: two posts with traffic passing between them.
        gateway: ['M5 4v16', 'M19 4v16', 'M9 12h6', 'M12.5 9l3 3-3 3'],
        // A route: one entry branching to several backends.
        route: ['M4 12h4', 'M8 12c4 0 4-6 8-6h4', 'M8 12c4 0 4 6 8 6h4', 'M20 6h.01', 'M20 18h.01'],
        // A grant: a permission crossing a boundary.
        grant: ['M4 5h7v14H4z', 'M15 12h5', 'M17.5 9.5l2.5 2.5-2.5 2.5'],
        // A custom resource: a piece that fits into the API.
        puzzle: [
            'M10 4h4v2.5a1.5 1.5 0 1 0 3 0V4h3v4h-2.5a1.5 1.5 0 1 0 0 3H20v4h-4v-2.5a1.5 1.5 0 1 0-3 0V15H9v-4H6.5a1.5 1.5 0 1 0 0-3H9V4z',
        ],

        // A terminal: a prompt and a cursor.
        terminal: ['M4 5h16v14H4z', 'M7.5 9.5l2.5 2.5-2.5 2.5', 'M13 15h4'],
        // A port forward: a local end, a remote end, and traffic crossing.
        forward: ['M4 8h5v8H4z', 'M15 8h5v8h-5z', 'M9 12h6', 'M13 10l2 2-2 2'],

        // Interface
        'chevron-left': ['M14.5 6l-6 6 6 6'],
        'chevron-right': ['M9.5 6l6 6-6 6'],
        'chevron-down': ['M6 9.5l6 6 6-6'],
        // Two offset frames: a set of identical copies of one pod.
        copies: ['M9 4h11v11', 'M4 9h11v11H4z'],
        // Two sheets, the front one whole: copy to the clipboard.
        copy: ['M9 9h11v11H9z', 'M5 15H4V4h11v1'],
        // A bare tick: done, as in copied.
        tick: ['M5 12.5l4.5 4.5L19 7.5'],
        // Growing and shrinking along one axis, which is what an HPA does.
        scale: ['M12 4v16', 'M8 8l4-4 4 4', 'M8 16l4 4 4-4'],
        // A dial: how much of an allowance is spent.
        gauge: ['M4 18a8 8 0 1 1 16 0', 'M12 18l4.5-5.5'],
        shield: ['M12 3l7 3v6c0 4.2-2.9 7.4-7 9-4.1-1.6-7-4.8-7-9V6l7-3z'],
        // Bars of increasing height: a rank order.
        priority: ['M6 20v-5', 'M12 20V9', 'M18 20V4'],
        // A chip with legs: the runtime a pod is handed to.
        chip: ['M8 8h8v8H8z', 'M5 5h14v14H5z', 'M9 2v3', 'M15 2v3', 'M9 19v3', 'M15 19v3'],
        // A plug: the cluster calling out to something else mid-request.
        webhook: ['M8 3v8a4 4 0 0 0 8 0V3', 'M12 15v6', 'M9 21h6'],
        // A rule sheet that has been checked.
        policy: ['M14 3H7a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V9l-6-6z', 'M13 3v6h6', 'M8.5 15l2.5 2.5 4.5-4.5'],
        // A chain link: a binding tying a policy to what it applies to.
        link: ['M10.5 13.5a4 4 0 0 0 5.7 0l2.3-2.3a4 4 0 0 0-5.7-5.7l-1.2 1.2', 'M13.5 10.5a4 4 0 0 0-5.7 0l-2.3 2.3a4 4 0 0 0 5.7 5.7l1.2-1.2'],
        // Three nodes and the lines between them: the access map, which draws
        // who holds what as a graph.
        graph: [
            'M6 3.5a2.5 2.5 0 1 0 0 5 2.5 2.5 0 0 0 0-5z',
            'M6 15.5a2.5 2.5 0 1 0 0 5 2.5 2.5 0 0 0 0-5z',
            'M18 9.5a2.5 2.5 0 1 0 0 5 2.5 2.5 0 0 0 0-5z',
            'M8.3 7l7.4 4',
            'M8.3 17l7.4-4',
        ],
        // One person, and several: a User and a Group subject.
        user: ['M12 4a4 4 0 1 0 0 8 4 4 0 0 0 0-8z', 'M4.5 20.5a7.5 7.5 0 0 1 15 0'],
        users: [
            'M9 5a3.5 3.5 0 1 0 0 7 3.5 3.5 0 0 0 0-7z',
            'M2.5 20a6.5 6.5 0 0 1 13 0',
            'M16 5.3a3.5 3.5 0 0 1 0 6.4',
            'M18 14.3a6.5 6.5 0 0 1 3.5 5.7',
        ],
        // A ship's wheel, which is what Helm is named for.
        helm: ['M12 4a8 8 0 1 0 0 16 8 8 0 0 0 0-16z', 'M12 9a3 3 0 1 0 0 6 3 3 0 0 0 0-6z', 'M12 4v5', 'M12 15v5', 'M4 12h5', 'M15 12h5'],
        refresh: ['M20.5 12a8.5 8.5 0 1 1-2.6-6.1', 'M20.5 4v5h-5'],
        // A tray with an arrow into it: a file coming down to this machine.
        download: ['M12 4v11', 'M7.5 10.5 12 15l4.5-4.5', 'M4 17v2a1 1 0 0 0 1 1h14a1 1 0 0 0 1-1v-2'],
        // Double chevrons, echoing the single chevron on each context's twisty:
        // one chevron opens a context, two open the lot.
        'expand-all': ['M6 5.5l6 6 6-6', 'M6 12.5l6 6 6-6'],
        'collapse-all': ['M6 11.5l6-6 6 6', 'M6 18.5l6-6 6 6'],
        // Bars shortening down the list with an arrow to match: the sidebar
        // sorted A to Z, and the same glyph flipped for Z to A. The third
        // state is neither, so its bars are uneven and it carries no arrow.
        'sort-asc': ['M4 6h11', 'M4 12h7', 'M4 18h4', 'M18 5v14', 'M15 16l3 3 3-3'],
        'sort-desc': ['M4 6h4', 'M4 12h7', 'M4 18h11', 'M18 19V5', 'M15 8l3-3 3 3'],
        'sort-off': ['M4 6h8', 'M4 12h14', 'M4 18h6'],
        plus: ['M12 5v14', 'M5 12h14'],
        minus: ['M5 12h14'],
        // A folder with a plus: add every kubeconfig in a directory.
        'folder-plus': [
            'M3 7a1 1 0 0 1 1-1h5l2 2.5h8a1 1 0 0 1 1 1V18a1 1 0 0 1-1 1H4a1 1 0 0 1-1-1V7z',
            'M12 11.5v5',
            'M9.5 14h5',
        ],
        folder: ['M3 7a1 1 0 0 1 1-1h5l2 2.5h8a1 1 0 0 1 1 1V18a1 1 0 0 1-1 1H4a1 1 0 0 1-1-1V7z'],
        close: ['M6.5 6.5l11 11', 'M17.5 6.5l-11 11'],
        // A small disc: an editor holding changes that are not in the cluster.
        // Drawn as a circle of almost no radius and filled out by the stroke,
        // so it stays a single-stroke glyph like everything else here.
        dot: ['M12 10.6a1.4 1.4 0 1 0 0 2.8 1.4 1.4 0 0 0 0-2.8z'],
        // A pencil: edit this object.
        edit: ['M4 20h4L18.5 9.5a2.83 2.83 0 0 0-4-4L4 16v4z', 'M13.5 6.5l4 4'],
        // A floppy disk, which is still what "save" looks like.
        save: ['M5 4h11l3 3v13H5z', 'M8.5 4v5h6.5V4', 'M8 20v-6h8v6'],
        'chevron-up': ['M6 14.5l6-6 6 6'],
        alert: ['M12 3.5l9.5 17h-19l9.5-17z', 'M12 9.5v4.5', 'M12 17.5h.01'],
        file: ['M14 3H7a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V8l-5-5z', 'M14 3v5h5'],
        search: ['M11 4.5a6.5 6.5 0 1 0 0 13 6.5 6.5 0 0 0 0-13z', 'M20 20l-4.4-4.4'],
        trash: ['M4 7h16', 'M9.5 7V4.5h5V7', 'M6.5 7l1 13h9l1-13'],
        undo: ['M3.5 8h11a5 5 0 0 1 0 10H8', 'M7.5 4l-4 4 4 4'],
        'dock-right': ['M4 5h16v14H4z', 'M14.5 5v14'],
        'dock-bottom': ['M4 5h16v14H4z', 'M4 14.5h16'],
        'dock-left': ['M4 5h16v14H4z', 'M9.5 5v14'],
        pin: ['M9 4h6', 'M10 4l-.5 7-3 2.5h11l-3-2.5-.5-7', 'M12 13.5V20'],

        // Settings
        // A cog: the app's own preferences, as against a cluster's contents.
        settings: [
            'M12 15a3 3 0 1 0 0-6 3 3 0 0 0 0 6z',
            'M19.4 15a1.6 1.6 0 0 0 .3 1.8l.1.1a2 2 0 1 1-2.8 2.8l-.1-.1a1.6 1.6 0 0 0-2.7 1.1V21a2 2 0 1 1-4 0v-.1A1.6 1.6 0 0 0 7.1 19.4l-.1.1a2 2 0 1 1-2.8-2.8l.1-.1a1.6 1.6 0 0 0-1.1-2.7H3a2 2 0 1 1 0-4h.1A1.6 1.6 0 0 0 4.6 7.1l-.1-.1a2 2 0 1 1 2.8-2.8l.1.1a1.6 1.6 0 0 0 1.8.3H9a1.6 1.6 0 0 0 1-1.5V3a2 2 0 1 1 4 0v.1a1.6 1.6 0 0 0 2.7 1.1l.1-.1a2 2 0 1 1 2.8 2.8l-.1.1a1.6 1.6 0 0 0-.3 1.8V9a1.6 1.6 0 0 0 1.5 1H21a2 2 0 1 1 0 4h-.1a1.6 1.6 0 0 0-1.5 1z',
        ],
        // A sun: the light theme.
        sun: ['M12 8a4 4 0 1 0 0 8 4 4 0 0 0 0-8z', 'M12 2v2', 'M12 20v2', 'M4.2 4.2l1.4 1.4', 'M18.4 18.4l1.4 1.4', 'M2 12h2', 'M20 12h2', 'M4.2 19.8l1.4-1.4', 'M18.4 5.6l1.4-1.4'],
        // A crescent: the dark theme.
        moon: ['M20 14.5A8.5 8.5 0 1 1 9.5 4a6.5 6.5 0 0 0 10.5 10.5z'],
        // A display: the theme the OS is asking for.
        monitor: ['M4 5h16v11H4z', 'M9 20h6', 'M12 16v4'],
        // A display with something on it: themes, and a terminal of your own.
        // The settings rail and the terminal picker have named this all
        // along; until now it drew as an empty box.
        display: ['M4 5h16v11H4z', 'M9 20h6', 'M12 16v4', 'M8 9h8', 'M8 12h5'],
        // Rows at two spacings: the table density.
        rows: ['M4 6h16', 'M4 12h16', 'M4 18h16'],
        // A table split down its verticals: which columns a listing shows.
        columns: ['M4 5h16v14H4z', 'M10 5v14', 'M15 5v14'],
        // A letter between size marks: the root font size.
        type: ['M5 6V4h14v2', 'M12 4v16', 'M9 20h6'],
        // A box being reopened: last session's tabs coming back.
        restore: ['M3 12a9 9 0 1 0 2.6-6.4', 'M3 4v5h5', 'M12 8v4l3 2'],
        // An open book: what the app is and where it keeps things.
        info: ['M12 3a9 9 0 1 0 0 18 9 9 0 0 0 0-18z', 'M12 11v5', 'M12 8h.01'],
        // A ring with a question mark: the help page.
        help: ['M12 3a9 9 0 1 0 0 18 9 9 0 0 0 0-18z', 'M9.5 9.5a2.5 2.5 0 1 1 3.5 2.3c-.7.4-1 .9-1 1.7', 'M12 17h.01'],
        // An open book: the Kubernetes primer.
        book: ['M12 6.5c-1.5-1.3-3.8-2-7-2v13c3.2 0 5.5.7 7 2 1.5-1.3 3.8-2 7-2v-13c-3.2 0-5.5.7-7 2z', 'M12 6.5v13'],
    };
</script>

<script lang="ts">
    interface Props {
        name: string;
        size?: number;
        /** Stroke width; thinner reads better at small sizes on dark backgrounds. */
        weight?: number;
    }

    let { name, size = 16, weight = 1.6 }: Props = $props();

    // An unknown name falls back to a generic glyph rather than rendering an
    // empty box, so a typo in a catalogue entry is visible but not disruptive.
    let paths = $derived(PATHS[name] ?? PATHS.box);
</script>

<svg
    class="icon"
    width={size}
    height={size}
    viewBox="0 0 24 24"
    fill="none"
    stroke="currentColor"
    stroke-width={weight}
    stroke-linecap="round"
    stroke-linejoin="round"
    aria-hidden="true"
>
    {#each paths as d (d)}
        <path {d} />
    {/each}
</svg>

<style>
    .icon {
        display: block;
        flex: 0 0 auto;
    }
</style>
