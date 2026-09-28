import { openUrl, revealItemInDir } from "@tauri-apps/plugin-opener";
import { toast } from "sonner";
import { errorMessage } from "@/lib/async";
import { currentPrivacy } from "@/lib/privacy";

// Opening something outside the app can fail (no browser, a moved folder);
// a click that does nothing reads as a dead button, so failures are shown.

/** openExternal opens a URL in the system browser or mail client. */
export function openExternal(url: string) {
  openUrl(url).catch((e: unknown) =>
    toast.error(`Couldn't open ${currentPrivacy().text(url)}`, { description: errorMessage(e) }),
  );
}

/** revealPath shows a file or folder in the system file manager. */
export function revealPath(path: string) {
  revealItemInDir(path).catch((e: unknown) =>
    toast.error("Couldn't show that folder", { description: `${currentPrivacy().path(path)}: ${errorMessage(e)}` }),
  );
}
