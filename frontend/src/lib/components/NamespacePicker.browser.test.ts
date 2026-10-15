import { expect, test, vi } from 'vitest';
import { page, userEvent } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';
import NamespacePicker from './NamespacePicker.svelte';

// The namespace filter as checkboxes: any number ticked, none meaning all.

const FEW = ['default', 'kube-system', 'monitoring'];
const MANY = ['a', 'b', 'c', 'd', 'e', 'f', 'g', 'h', 'i'];

const trigger = () => page.getByRole('button', { name: /^Namespaces/ });

test('nothing ticked reads as every namespace', async () => {
    render(NamespacePicker, { namespaces: FEW, selected: [], onchange: vi.fn() });

    await expect.element(trigger()).toMatchTextContent('All namespaces');
});

test('ticking a row adds it to the choice, and the panel stays open for the next one', async () => {
    const onchange = vi.fn();
    render(NamespacePicker, { namespaces: FEW, selected: ['default'], onchange });

    await trigger().click();
    await page.getByRole('menuitemcheckbox', { name: 'kube-system' }).click();

    expect(onchange).toHaveBeenLastCalledWith(['default', 'kube-system']);
    await expect.element(page.getByRole('menu', { name: 'Namespaces' })).toBeVisible();
});

test('ticking a ticked row takes it out', async () => {
    const onchange = vi.fn();
    render(NamespacePicker, { namespaces: FEW, selected: ['default', 'monitoring'], onchange });

    await trigger().click();
    await expect.element(page.getByRole('menuitemcheckbox', { name: 'default' })).toHaveAttribute('aria-checked', 'true');
    await page.getByRole('menuitemcheckbox', { name: 'default' }).click();

    expect(onchange).toHaveBeenLastCalledWith(['monitoring']);
});

test('All namespaces clears the choice', async () => {
    const onchange = vi.fn();
    render(NamespacePicker, { namespaces: FEW, selected: ['default', 'monitoring'], onchange });

    await trigger().click();
    await page.getByRole('menuitemcheckbox', { name: 'All namespaces' }).click();

    expect(onchange).toHaveBeenLastCalledWith([]);
});

test('the trigger names two namespaces and counts more', async () => {
    render(NamespacePicker, { namespaces: FEW, selected: ['default', 'kube-system'], onchange: vi.fn() });
    await expect.element(trigger()).toMatchTextContent('default, kube-system');

    document.body.innerHTML = '';
    render(NamespacePicker, { namespaces: FEW, selected: FEW, onchange: vi.fn() });
    await expect.element(trigger()).toMatchTextContent('3 namespaces');
});

test('a filter box appears once there are many, and narrows the rows', async () => {
    render(NamespacePicker, { namespaces: MANY, selected: [], onchange: vi.fn() });

    await trigger().click();
    await page.getByRole('searchbox', { name: 'Filter namespaces' }).fill('b');

    await expect.element(page.getByRole('menuitemcheckbox', { name: 'b', exact: true })).toBeVisible();
    expect(page.getByRole('menuitemcheckbox', { name: 'a', exact: true }).elements()).toHaveLength(0);
});

test('a few namespaces get no filter box', async () => {
    render(NamespacePicker, { namespaces: FEW, selected: [], onchange: vi.fn() });

    await trigger().click();
    await expect.element(page.getByRole('menuitemcheckbox', { name: 'default' })).toBeVisible();

    expect(page.getByRole('searchbox').elements()).toHaveLength(0);
});

test('Escape closes the panel and puts focus back on the trigger', async () => {
    render(NamespacePicker, { namespaces: FEW, selected: [], onchange: vi.fn() });

    await trigger().click();
    await expect.element(page.getByRole('menu', { name: 'Namespaces' })).toBeVisible();
    await userEvent.keyboard('{Escape}');

    expect(page.getByRole('menu').elements()).toHaveLength(0);
    await expect.element(trigger()).toHaveFocus();
});

// A namespace deleted after it was ticked would otherwise narrow the table
// from somewhere nobody can see.
test('a ticked namespace the cluster no longer has stays listed, so it can be unticked', async () => {
    const onchange = vi.fn();
    render(NamespacePicker, { namespaces: FEW, selected: ['gone'], onchange });

    await trigger().click();
    await expect.element(page.getByRole('menuitemcheckbox', { name: 'gone' })).toHaveAttribute('aria-checked', 'true');
    await page.getByRole('menuitemcheckbox', { name: 'gone' }).click();

    expect(onchange).toHaveBeenLastCalledWith([]);
});
