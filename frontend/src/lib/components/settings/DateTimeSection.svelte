<!--
  How dates and times are written: the clock, the date, the zone, and whether
  a table's time column says how long ago or when.

  One choice for the whole window and for every plugin's pages, which are
  handed it through the bridge -- a 24-hour clock in the app and a 12-hour one
  in a plugin would be worse than either. See datetime.svelte.ts.
-->
<script lang="ts">
    import { workspace } from '../../state/workspace.svelte';
    import { formatDate, formatDateTime, type DateTimeSettings } from '../../datetime.svelte';
    import SegmentedControl from './SegmentedControl.svelte';
    import SettingsRow from './SettingsRow.svelte';
    import SettingsSection from './SettingsSection.svelte';

    const CLOCKS = [
        { value: 'system', label: 'System' },
        { value: '24h', label: '24-hour' },
        { value: '12h', label: '12-hour' },
    ];

    const DATES: { value: DateTimeSettings['dates']; label: string }[] = [
        { value: 'system', label: 'System' },
        { value: 'iso', label: 'ISO' },
        { value: 'dmy', label: 'Day first' },
        { value: 'mdy', label: 'Month first' },
        { value: 'long', label: 'In words' },
    ];

    const ZONES = [
        { value: 'local', label: 'This computer' },
        { value: 'utc', label: 'UTC' },
    ];

    const AGES = [
        { value: 'relative', label: 'How long ago' },
        { value: 'absolute', label: 'Date and time' },
    ];

    let settings = $derived(workspace.settings.preferences.dateTime);

    // The preview ticks, so the seconds show the clock is the one chosen.
    let now = $state(new Date());
    $effect(() => {
        const timer = setInterval(() => (now = new Date()), 1000);
        return () => clearInterval(timer);
    });
</script>

<SettingsSection title="Dates and times">
    <p class="preview" aria-live="off">
        <span class="label">Now reads as</span>
        <span class="sample">{formatDateTime(now, { seconds: true })}</span>
    </p>

    <SettingsRow label="Clock" hint="System follows this computer's language and region settings.">
        <SegmentedControl
            label="Clock"
            options={CLOCKS}
            value={settings.clock}
            onchange={(v) => workspace.setDateTime({ clock: v as DateTimeSettings['clock'] })}
        />
    </SettingsRow>

    <SettingsRow label="Dates" hint="How a date is written. Today reads as {formatDate(now)}.">
        <SegmentedControl
            label="Dates"
            options={DATES}
            value={settings.dates}
            onchange={(v) => workspace.setDateTime({ dates: v as DateTimeSettings['dates'] })}
        />
    </SettingsRow>

    <SettingsRow
        label="Time zone"
        hint="UTC is what the cluster's own logs and events are written in, which makes the two easier to line up. It is marked UTC wherever a time is shown with its date."
    >
        <SegmentedControl
            label="Time zone"
            options={ZONES}
            value={settings.zone}
            onchange={(v) => workspace.setDateTime({ zone: v as DateTimeSettings['zone'] })}
        />
    </SettingsRow>

    <SettingsRow
        label="Times in tables"
        hint="An Age or Last Seen column as kubectl writes it, “5m”, or as the moment itself. Whichever you choose, the other is shown when you point at it; sorting is the same either way."
    >
        <SegmentedControl
            label="Times in tables"
            options={AGES}
            value={settings.ages}
            onchange={(v) => workspace.setDateTime({ ages: v as DateTimeSettings['ages'] })}
        />
    </SettingsRow>

    <p class="note">Plugins are handed the same choices, so their pages write dates and times as the app does.</p>
</SettingsSection>

<style>
    .preview {
        display: flex;
        align-items: baseline;
        gap: 10px;
        margin: 0 0 14px;
        padding: 10px 12px;
        border-radius: var(--radius);
        background: var(--bg-panel);
        border: 1px solid var(--border-soft);
    }

    .preview .label {
        font-size: 12px;
        color: var(--text-faint);
    }

    .preview .sample {
        font-family: var(--mono);
        font-size: 14px;
        color: var(--text);
        font-variant-numeric: tabular-nums;
    }

    .note {
        margin: 14px 0 0;
        font-size: 12px;
        color: var(--text-dim);
        line-height: 1.5;
    }
</style>
