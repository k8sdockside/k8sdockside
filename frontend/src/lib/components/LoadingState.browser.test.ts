import { afterEach, expect, test, vi } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';
import LoadingState from './LoadingState.svelte';

afterEach(() => {
    vi.useRealTimers();
});

test('it says which step it is on', async () => {
    const { rerender } = await render(LoadingState, { what: 'pods', cluster: 'prod', phase: 'connecting' });
    await expect.element(page.getByText('Connecting to prod…')).toBeVisible();

    await rerender({ what: 'pods', cluster: 'prod', phase: 'reading' });
    await expect.element(page.getByText('Reading pods from prod…')).toBeVisible();
});

// A slow answer looks like a hung one unless something says otherwise.
test('a long wait says how long, why it can be, and then offers to try again', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    const retry = vi.fn();
    await render(LoadingState, { what: 'Helm releases', cluster: 'prod', phase: 'reading', onRetry: retry });
    expect(page.getByText(/taking longer than usual/).elements()).toHaveLength(0);

    vi.advanceTimersByTime(12_000);
    await expect.element(page.getByText(/taking longer than usual/)).toBeVisible();
    await expect.element(page.getByText(/1[0-9] s/)).toBeVisible();
    expect(page.getByRole('button', { name: 'Try again' }).elements()).toHaveLength(0);

    vi.advanceTimersByTime(20_000);
    await expect.element(page.getByText(/Still no answer from prod/)).toBeVisible();
    await page.getByRole('button', { name: 'Try again' }).click();
    expect(retry).toHaveBeenCalledOnce();
});
