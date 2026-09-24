import { afterEach, expect, test, vi } from 'vitest';
import { everyWhileVisible } from './visibility';

function setHidden(hidden: boolean): void {
    Object.defineProperty(document, 'visibilityState', { configurable: true, get: () => (hidden ? 'hidden' : 'visible') });
    document.dispatchEvent(new Event('visibilitychange'));
}

afterEach(() => {
    setHidden(false);
    vi.useRealTimers();
});

test('it runs on its interval while the window is visible', () => {
    vi.useFakeTimers();
    const fn = vi.fn();
    const stop = everyWhileVisible(1000, fn);
    vi.advanceTimersByTime(3000);
    expect(fn).toHaveBeenCalledTimes(3);
    stop();
});

// Nobody sees a refresh behind a minimised window; what comes back into view
// is brought up to date at once rather than at the next turn.
test('it skips its turns while hidden, and catches up once when shown', () => {
    vi.useFakeTimers();
    const fn = vi.fn();
    const stop = everyWhileVisible(1000, fn);
    setHidden(true);
    vi.advanceTimersByTime(5000);
    expect(fn).not.toHaveBeenCalled();

    setHidden(false);
    expect(fn).toHaveBeenCalledTimes(1);
    stop();
});

test('shown again without having missed a turn, it does not run early', () => {
    vi.useFakeTimers();
    const fn = vi.fn();
    const stop = everyWhileVisible(1000, fn);
    setHidden(true);
    setHidden(false);
    expect(fn).not.toHaveBeenCalled();
    stop();
});

test('stopped, it never runs again', () => {
    vi.useFakeTimers();
    const fn = vi.fn();
    everyWhileVisible(1000, fn)();
    vi.advanceTimersByTime(5000);
    setHidden(true);
    setHidden(false);
    expect(fn).not.toHaveBeenCalled();
});
