import { defineConfig } from 'vitest/config';
import { svelte } from '@sveltejs/vite-plugin-svelte';
import { playwright } from '@vitest/browser-playwright';

// Two projects, because the two things worth testing here want different
// environments.
//
// The application state is plain logic over runes and is fastest in jsdom with
// the Wails bindings mocked. Components are not: `$effect` and the runes
// runtime only behave as they do in the app when there is a real browser under
// them, and the tab menu is almost entirely effects, focus and pointer events.
export default defineConfig({
    plugins: [svelte()],
    test: {
        projects: [
            {
                extends: true,
                test: {
                    name: 'state',
                    environment: 'jsdom',
                    include: ['src/**/*.test.ts'],
                    exclude: ['src/**/*.browser.test.ts'],
                },
            },
            {
                extends: true,
                test: {
                    name: 'components',
                    include: ['src/**/*.browser.test.ts'],
                    browser: {
                        enabled: true,
                        provider: playwright(),
                        headless: true,
                        instances: [{ browser: 'chromium' }],
                        // vitest 5 made locators match whole text by default; the
                        // tests find elements by a phrase inside them.
                        locators: { exact: false },
                    },
                },
            },
        ],
    },
});
