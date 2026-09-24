import { expect, test } from 'vitest';
import { isSnoozed, snoozeChoices, tomorrowMorning } from './snooze';

test('tomorrow morning is eight the next day, local time', () => {
    const now = new Date(2026, 8, 24, 23, 30);
    expect(tomorrowMorning(now)).toEqual(new Date(2026, 8, 25, 8, 0, 0, 0));
});

test('the choices are an hour, four hours and tomorrow morning', () => {
    const now = new Date(2026, 8, 24, 14, 0);
    const [hour, four, morning] = snoozeChoices(now);
    expect(hour!.until.getTime() - now.getTime()).toBe(3600_000);
    expect(four!.until.getTime() - now.getTime()).toBe(4 * 3600_000);
    expect(morning!.until).toEqual(new Date(2026, 8, 25, 8, 0, 0, 0));
});

test('a snooze is on until it ends', () => {
    expect(isSnoozed(0)).toBe(false);
    expect(isSnoozed(Date.now() + 1000)).toBe(true);
    expect(isSnoozed(Date.now() - 1000)).toBe(false);
});
