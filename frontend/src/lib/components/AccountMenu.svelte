<!--
  Who is signed in, in the title bar of the web version.

  Only there: the desktop app has one user, who is whoever is at the machine,
  and nothing to sign in to or out of. In the web version the window is one
  user's view of a server several people share, and these are the ways out of
  it -- to the gateway's own pages for the account, the administration and
  signing out.

  The items are plain links rather than buttons that ask the runtime to open
  something. They are pages of the same site, meant to replace this one in the
  same tab the way any site's account menu does, and a link is the one thing a
  browser already knows how to do that with: middle-click, copy the address,
  open in a new tab. Drawn like the View menu beside it -- see ViewMenu.svelte.
-->
<script lang="ts">
    import { session } from '../state/session.svelte';
    import Icon from './Icon.svelte';

    let open = $state(false);
    let menuEl = $state<HTMLElement | null>(null);
    let buttonEl = $state<HTMLButtonElement | null>(null);

    /**
     * What the menu offers, left out where the backend gave no page for it.
     * Administration is for administrators only: the gateway would refuse
     * anyone else, and a link that leads to "not allowed" is not an offer.
     */
    let items = $derived(
        [
            { label: 'Account', href: session.accountUrl, icon: 'user', shown: true },
            { label: 'Administration', href: session.adminUrl, icon: 'shield', shown: session.admin },
            { label: 'Sign out', href: session.logoutUrl, icon: 'power', shown: true },
        ].filter((item) => item.shown && item.href !== ''),
    );

    function close(): void {
        open = false;
    }

    function onKeyDown(event: KeyboardEvent): void {
        if (event.key === 'Escape') {
            event.stopPropagation();
            close();
            buttonEl?.focus();
            return;
        }
        if (event.key !== 'ArrowDown' && event.key !== 'ArrowUp') return;

        event.preventDefault();
        const links = [...(menuEl?.querySelectorAll('a') ?? [])];
        const at = links.indexOf(document.activeElement as HTMLAnchorElement);
        const next = (at + (event.key === 'ArrowDown' ? 1 : -1) + links.length) % links.length;
        (links[next] as HTMLAnchorElement | undefined)?.focus();
    }

    // Focus the first item, so the menu is usable without the mouse that opened it.
    $effect(() => {
        if (open && menuEl) menuEl.querySelector<HTMLAnchorElement>('a')?.focus();
    });
</script>

<svelte:window onclick={close} onresize={close} />

<!-- The click that opens the menu must not reach the window handler that
     closes it again, and neither must a click on an item inside it. Everything
     inside is a button or a link, so there is nothing here for a keyboard to
     need a handler for. -->
<!-- svelte-ignore a11y_no_static_element_interactions -->
<!-- svelte-ignore a11y_click_events_have_key_events -->
<div class="host" onclick={(e) => e.stopPropagation()}>
    <button
        class="trigger"
        bind:this={buttonEl}
        aria-haspopup="menu"
        aria-expanded={open}
        aria-label="Account: {session.displayName}"
        title={session.username && session.username !== session.displayName
            ? `${session.displayName} (${session.username})`
            : session.displayName}
        onclick={() => (open = !open)}
    >
        <Icon name="user" size={14} />
        <span class="who">{session.displayName}</span>
        <Icon name="chevron-down" size={12} />
    </button>

    {#if open}
        <!-- A menu is not focusable itself -- its items are, and the effect
             above puts the focus on one as it opens. -->
        <!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
        <!-- svelte-ignore a11y_interactive_supports_focus -->
        <div class="menu" role="menu" aria-label="Account" bind:this={menuEl} onkeydown={onKeyDown}>
            {#if session.username}
                <p class="signed-in">Signed in as <span class="name">{session.username}</span></p>
                <hr />
            {/if}
            {#each items as item (item.label)}
                <a role="menuitem" href={item.href}>
                    <span class="tick"><Icon name={item.icon} size={13} /></span>
                    <span class="label">{item.label}</span>
                </a>
            {/each}
        </div>
    {/if}
</div>

<style>
    .host {
        position: relative;
        display: flex;
        align-items: center;
        /* The title bar drags the window; a menu in it must not. */
        --wails-draggable: no-drag;
        z-index: 5;
    }

    .trigger {
        display: flex;
        align-items: center;
        gap: 5px;
        height: 24px;
        max-width: 200px;
        padding: 0 7px 0 8px;
        border-radius: var(--radius-sm);
        font-size: 12px;
        color: var(--text-dim);
    }

    .trigger:hover,
    .trigger[aria-expanded='true'] {
        background: var(--bg-hover);
        color: var(--text);
    }

    .who {
        min-width: 0;
        overflow: hidden;
        text-overflow: ellipsis;
        white-space: nowrap;
    }

    .menu {
        position: absolute;
        top: calc(100% + 4px);
        /* Anchored to the trigger's right edge for the reason the View menu
           is: this sits at the right end of the title bar. */
        right: 0;
        min-width: 200px;
        padding: 4px;
        border: 1px solid var(--border);
        border-radius: var(--radius);
        background: var(--bg-raised);
        box-shadow: 0 8px 24px rgb(0 0 0 / 0.35);
    }

    .signed-in {
        margin: 4px 8px;
        font-size: 11px;
        color: var(--text-faint);
        overflow: hidden;
        text-overflow: ellipsis;
        white-space: nowrap;
    }

    .signed-in .name {
        color: var(--text-dim);
    }

    .menu a {
        display: flex;
        align-items: center;
        gap: 8px;
        width: 100%;
        height: 26px;
        padding: 0 8px;
        border-radius: var(--radius-sm);
        font-size: 12px;
        color: var(--text);
        text-decoration: none;
    }

    .menu a:hover,
    .menu a:focus-visible {
        background: var(--bg-hover);
        outline: none;
    }

    .tick {
        display: grid;
        place-items: center;
        width: 14px;
        flex: 0 0 auto;
        color: var(--text-dim);
    }

    .label {
        flex: 1 1 auto;
    }

    hr {
        height: 1px;
        margin: 4px 6px;
        border: 0;
        background: var(--border-soft);
    }
</style>
