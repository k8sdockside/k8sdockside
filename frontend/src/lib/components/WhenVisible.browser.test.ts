import { expect, test } from 'vitest';
import { render } from 'vitest-browser-svelte';
import Probe from './testing/WhenVisibleProbe.svelte';

const frame = () => new Promise((r) => requestAnimationFrame(() => setTimeout(r, 30)));

// Charts and plugin panels below the fold start their work when they are about
// to be seen, not when the page opens.
test('what it holds is drawn only once it is scrolled near', async () => {
    await render(Probe);
    await frame();
    expect(document.querySelector('.late')).toBeNull();

    const box = document.querySelector<HTMLElement>('.box')!;
    box.scrollTop = box.scrollHeight;
    await frame();
    await frame();
    expect(document.querySelector('.late')?.textContent).toBe('drawn');

    // And it stays, scrolled away again.
    box.scrollTop = 0;
    await frame();
    expect(document.querySelector('.late')).not.toBeNull();
});
