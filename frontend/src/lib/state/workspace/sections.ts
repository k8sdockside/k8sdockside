// The order of the details panel's sections.
//
// The panel draws its sections in a default order -- resources, charts, a
// machine's own panel, conditions, related objects, pods, the panels plugins
// bring, the report -- and a user can rearrange them. What they arranged is
// kept as one list of section keys for every object on every cluster, the
// same keys their folds are kept under ("describe", "plugin:<id>/<section>").
//
// One list for every kind, while each kind shows only some of the sections,
// is what makes the rules below necessary:
//
//   - a section the list has never seen is not "new" if it is the app's own:
//     a pod has no virtual machine panel, so an order arranged on a pod does
//     not mention it, and a machine opened next must still get it where it
//     belongs. The app's own sections fall back to their default place,
//     next to the neighbour they follow by default.
//   - a plugin's section the list has never seen *is* new -- a plugin
//     installed since -- and goes where the user said new ones go: at the
//     bottom (the default), or where the plugin's sections are by default.
//   - rearranging the sections of one kind must not lose the place of those
//     it did not show. `rearranged` moves only the keys on screen, in the
//     slots they already had.

/** Where a plugin section new to the user's order goes. */
export type NewSectionPlace = 'bottom' | 'default';

export const NEW_SECTION_PLACES: NewSectionPlace[] = ['bottom', 'default'];

/** Whether a section is one a plugin brings, rather than the app's own. */
export function isPluginSection(key: string): boolean {
    return key.startsWith('plugin:');
}

/**
 * `available` -- the keys this object has, in the default order -- in the
 * order the user chose.
 */
export function orderSections(available: readonly string[], stored: readonly string[] | null | undefined, place: NewSectionPlace = 'bottom'): string[] {
    if (!stored || stored.length === 0) return [...available];
    const here = new Set(available);
    const out = stored.filter((k, i) => here.has(k) && stored.indexOf(k) === i);
    const bottom: string[] = [];
    for (let i = 0; i < available.length; i++) {
        const key = available[i]!;
        if (out.includes(key)) continue;
        if (isPluginSection(key) && place === 'bottom') {
            bottom.push(key);
            continue;
        }
        // After the nearest section that comes before it by default and is
        // already placed; first, if none is.
        let at = 0;
        for (let j = i - 1; j >= 0; j--) {
            const before = out.indexOf(available[j]!);
            if (before >= 0) {
                at = before + 1;
                break;
            }
        }
        out.splice(at, 0, key);
    }
    return [...out, ...bottom];
}

/**
 * The stored order after the sections on screen were rearranged from
 * `before` to `after` (the same keys, in a new order). Keys not on screen
 * keep their slots; the keys on screen fill theirs in the new order. A key
 * on screen the stored list did not have yet is given a slot where it was
 * shown.
 */
export function rearranged(stored: readonly string[] | null | undefined, before: readonly string[], after: readonly string[]): string[] {
    // The full order as it stood: the stored list with any key it lacked
    // slotted in after the key it followed on screen.
    const base = [...(stored ?? [])].filter((k, i, all) => all.indexOf(k) === i);
    for (let i = 0; i < before.length; i++) {
        const key = before[i]!;
        if (base.includes(key)) continue;
        const prev = i > 0 ? base.indexOf(before[i - 1]!) : -1;
        base.splice(prev + 1, 0, key);
    }
    const onScreen = new Set(before);
    const queue = after.filter((k) => onScreen.has(k));
    return base.map((k) => (onScreen.has(k) ? queue.shift()! : k));
}
