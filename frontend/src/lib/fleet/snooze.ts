// The ways the cluster alerts can be snoozed, and how long each lasts.

export interface SnoozeChoice {
    label: string;
    until: Date;
}

/** When "tomorrow morning" is: 08:00 local time the next day. */
export function tomorrowMorning(now = new Date()): Date {
    const at = new Date(now);
    at.setDate(at.getDate() + 1);
    at.setHours(8, 0, 0, 0);
    return at;
}

/** The snoozes offered, shortest first. */
export function snoozeChoices(now = new Date()): SnoozeChoice[] {
    return [
        { label: 'For 1 hour', until: new Date(now.getTime() + 60 * 60 * 1000) },
        { label: 'For 4 hours', until: new Date(now.getTime() + 4 * 60 * 60 * 1000) },
        { label: 'Until tomorrow morning', until: tomorrowMorning(now) },
    ];
}

/** Whether a snooze ending at until (a timestamp, 0 for none) is still on. */
export function isSnoozed(until: number, now = Date.now()): boolean {
    return until > now;
}
