// Which version of the app a documentation page is drawn for.
//
// Help is one page for both versions. Most of it is true of both; what is not
// is marked, section by section or block by block, with `only`, and the version
// it is not meant for leaves it out. That keeps the web version from telling
// anyone to pick a file from a disk its browser cannot see, and the desktop app
// from explaining a sign-in it does not have.

import type { Mode, Page } from './types';

/**
 * The page as one version of the app shows it: every section and block marked
 * for the other version left out, and everything unmarked kept.
 */
export function forMode(page: Page, server: boolean): Page {
    const mode: Mode = server ? 'web' : 'desktop';
    const kept = (only?: Mode) => only === undefined || only === mode;
    return {
        ...page,
        sections: page.sections
            .filter((section) => kept(section.only))
            .map((section) => ({ ...section, blocks: section.blocks.filter((block) => kept(block.only)) })),
    };
}
