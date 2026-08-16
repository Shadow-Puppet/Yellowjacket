/**
 * "Ask the user for a folder", once.
 *
 * Three call sites want a directory — the first-run wizard, the library
 * settings and the download clients' save path — and on desktop all
 * three can use the platform's own dialog. On Android that dialog
 * *returns an error*: the Storage Access Framework yields tree URIs
 * rather than filesystem paths, and a path is what this app's library
 * model is keyed on.
 *
 * So the platform test lives here rather than at each call site, which
 * is the same rule `utils/binding.ts` and `utils/library-status.ts`
 * follow: a fact about the platform is stated once, at the boundary.
 *
 * *Which* platform is asked of the backend, not of the Wails runtime's
 * `System.IsAndroid()`. The dialog is backend code, so the backend is
 * what knows whether it can open one; it answers for iOS at the same
 * time; and it keeps this testable through the ordinary transport fake
 * rather than a module mock.
 *
 * Returns the chosen path, or `null` if the user cancelled. Callers
 * treat those the same way they always did — a falsy result means "no
 * change" — so adopting this is a one-line edit at each site.
 */
import {
  DirectoryPicker,
  HasNativeDirectoryPicker,
} from "@go/frontendutil/frontendutil.js";

import type { FolderPicker } from "../components/folder-picker/folder-picker";

let picker: FolderPicker | null = null;
let native: Promise<boolean> | null = null;

/**
 * Asked once per session and remembered: it cannot change while the app
 * is running, and a folder picker should not pay a round trip to find
 * out which kind it is.
 */
function hasNative(): Promise<boolean> {
  native ??= HasNativeDirectoryPicker().catch(() => true);

  return native;
}

/** Testing seam: forget the cached platform answer. */
export function resetDirectoryPickerCache(): void {
  native = null;
  picker = null;
}

/**
 * The in-app browser is mounted on first use and then kept.
 *
 * Mounting it on demand and awaiting its module in the same update as
 * `showModal()` is the trap `index.ts` documents for views — so the
 * element is created, appended and *then* asked to open, on separate
 * turns.
 */
async function inAppPicker(): Promise<FolderPicker> {
  if (picker) return picker;

  await import("../components/folder-picker/folder-picker");

  const el = document.createElement("folder-picker");

  document.body.appendChild(el);
  picker = el;

  return el;
}

/** Ask for a directory. Resolves to an absolute path, or null. */
export async function pickDirectory(startAt?: string): Promise<string | null> {
  if (!(await hasNative())) {
    const el = await inAppPicker();

    return el.choose(startAt);
  }

  // The desktop dialog returns '' when dismissed; normalise that to
  // null so every caller has one falsy case to handle rather than
  // two.
  const chosen = await DirectoryPicker();

  return chosen === "" ? null : chosen;
}
