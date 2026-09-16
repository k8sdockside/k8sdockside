/**
 * Puts text on the clipboard of the machine the user is sitting at.
 *
 * The browser's clipboard, not the Wails runtime's: in the web version the
 * runtime's clipboard is the pod's. The async API needs a secure context and
 * focus; where it is refused, the old copy command still works from a click.
 * Resolves with whether the text was copied.
 */
export async function copyText(text: string): Promise<boolean> {
    try {
        await navigator.clipboard.writeText(text);
        return true;
    } catch {
        const area = document.createElement('textarea');
        area.value = text;
        area.setAttribute('readonly', '');
        area.style.position = 'fixed';
        area.style.opacity = '0';
        document.body.appendChild(area);
        area.select();
        try {
            return document.execCommand('copy');
        } catch {
            return false;
        } finally {
            area.remove();
        }
    }
}
