import { beforeEach, expect, test, vi } from 'vitest';
import { page, userEvent } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';

const VMScreenshot = vi.hoisted(() => vi.fn());
const keyboard = vi.hoisted(() => ({ Open: vi.fn(), Send: vi.fn(), Close: vi.fn() }));
vi.mock('../../../bindings/github.com/k8sdockside/k8sdockside/internal/services', () => ({
    ResourceService: { VMScreenshot },
    KubeconfigService: { Sync: vi.fn().mockResolvedValue([]), Files: vi.fn().mockResolvedValue([]) },
    SettingsService: {
        Get: vi.fn().mockResolvedValue({}),
        ConfigPath: vi.fn().mockResolvedValue(''),
        SetContextPrefs: vi.fn().mockResolvedValue({}),
        SetPanes: vi.fn().mockResolvedValue({}),
        SetLayout: vi.fn().mockResolvedValue({}),
        SetPreferences: vi.fn().mockResolvedValue({}),
    },
    PluginService: {
        List: vi.fn().mockResolvedValue({ plugins: [], dir: '', folders: [], problems: [] }),
    },
    MetricsService: { Attachments: vi.fn().mockResolvedValue([]) },
    ThemeService: { List: vi.fn().mockResolvedValue({ themes: [], dir: '', folders: [], problems: [] }) },
    // The workspace store imports the whole services module, so every service
    // it touches has to exist here even though this panel calls none of them.
    HelmService: { Tool: vi.fn().mockResolvedValue({ found: false, path: '', version: '', configured: false, reason: '' }) },
    LogService: { Containers: vi.fn().mockResolvedValue([]), Close: vi.fn() },
    TerminalService: {
        Close: vi.fn(),
        Externals: vi.fn().mockResolvedValue({ terminals: [], kubectl: '', reason: '' }),
        OpenVMKeyboard: keyboard.Open,
        SendVMKeys: keyboard.Send,
        CloseVMKeyboard: keyboard.Close,
    },
    PortForwardService: { List: vi.fn().mockResolvedValue([]) },
    ActionService: { ObjectState: vi.fn().mockResolvedValue({ scalable: false, replicas: 0, cordoned: false, containers: [] }) },
    UpdateService: { Check: vi.fn().mockResolvedValue(null) },
}));

const VMScreen = (await import('./VMScreen.svelte')).default;

const TAB = {
    id: 'screen:cfg::prod#crd:virtualmachines.kubevirt.io#vms#win11',
    view: 'screen' as const,
    contextId: 'cfg::prod',
    kind: 'crd:virtualmachines.kubevirt.io',
    namespace: 'vms',
    name: 'win11',
    title: 'win11',
};

// One transparent pixel: enough for an <img> to have a picture to show.
const PIXEL =
    'data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNkYAAAAAYAAjCB0C8AAAAASUVORK5CYII=';

beforeEach(() => {
    document.body.innerHTML = '';
    VMScreenshot.mockReset();
    keyboard.Open.mockReset().mockResolvedValue('kbd-1');
    keyboard.Send.mockReset().mockResolvedValue(undefined);
    keyboard.Close.mockReset();
});

test('the display is shown, and read again while it is live', async () => {
    VMScreenshot.mockResolvedValue(PIXEL);
    render(VMScreen, { tab: TAB });

    await expect.element(page.getByRole('img', { name: 'The display of win11' })).toBeVisible();
    expect(VMScreenshot).toHaveBeenCalledWith('cfg::prod', 'vms', 'win11');
    await expect.poll(() => VMScreenshot.mock.calls.length, { timeout: 3000 }).toBeGreaterThan(1);
});

test('pausing holds the picture', async () => {
    VMScreenshot.mockResolvedValue(PIXEL);
    render(VMScreen, { tab: TAB });
    await expect.element(page.getByText('Live')).toBeVisible();

    await page.getByRole('button', { name: /Pause/ }).click();
    const calls = VMScreenshot.mock.calls.length;
    await new Promise((r) => setTimeout(r, 1500));

    expect(VMScreenshot.mock.calls.length).toBe(calls);
});

// A machine that is not running has no screen: that is said, not shown as black.
test('a display that cannot be read says why', async () => {
    VMScreenshot.mockRejectedValue(new Error('win11 is not running -- there is no screen to show'));
    render(VMScreen, { tab: TAB });

    await expect.element(page.getByText(/is not running/)).toBeVisible();
    await expect.element(page.getByText('Failed')).toBeVisible();
});

// The point of it: F-keys and arrows for a Talos dashboard reach the guest.
test('keys typed on the display go to the virtual machine, in order', async () => {
    VMScreenshot.mockResolvedValue(PIXEL);
    render(VMScreen, { tab: TAB });
    const screen = page.getByRole('application');
    await expect.element(screen).toBeVisible();

    (screen.element() as HTMLElement).focus();
    await userEvent.keyboard('{F3}{ArrowDown}');

    await expect.poll(() => keyboard.Send.mock.calls.length).toBe(4);
    expect(keyboard.Open).toHaveBeenCalledTimes(1);
    expect(keyboard.Open).toHaveBeenCalledWith('cfg::prod', 'vms', 'win11');
    expect(keyboard.Send.mock.calls.map((c) => c[1][0])).toEqual([
        { keysym: 0xffc0, down: true },
        { keysym: 0xffc0, down: false },
        { keysym: 0xff54, down: true },
        { keysym: 0xff54, down: false },
    ]);
});

test('the key strip sends what the keyboard here cannot', async () => {
    VMScreenshot.mockResolvedValue(PIXEL);
    render(VMScreen, { tab: TAB });

    await page.getByRole('button', { name: 'F2', exact: true }).click();

    await expect.poll(() => keyboard.Send.mock.calls.length).toBe(1);
    expect(keyboard.Send.mock.calls[0]![1]).toEqual([
        { keysym: 0xffbf, down: true },
        { keysym: 0xffbf, down: false },
    ]);
});
