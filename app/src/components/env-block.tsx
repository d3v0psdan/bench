import { useState } from "react";
import { ClipboardTextIcon, EyeIcon, EyeSlashIcon } from "@phosphor-icons/react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { errorMessage } from "@/lib/async";
import { isSecretEnv, usePrivacy } from "@/lib/privacy";

/** EnvBlock shows .env lines with credentials masked, a reveal toggle and a
 *  copy button that always copies the real values. Privacy mode takes the
 *  reveal toggle away. */
export function EnvBlock({ title, lines, copiedMessage }: { title: string; lines: string[]; copiedMessage: string }) {
  const [revealPicked, setReveal] = useState(false);
  const privacy = usePrivacy();
  const reveal = revealPicked && !privacy.secrets;
  const hasSecrets = lines.some((l) => isSecretEnv(l.split("=")[0], l.split("=").slice(1).join("=")));

  const copy = () =>
    navigator.clipboard.writeText(lines.join("\n")).then(
      () => toast.success(copiedMessage),
      (e: unknown) => toast.error("Couldn't copy", { description: errorMessage(e) }),
    );

  return (
    <section>
      <div className="mb-2 flex items-center justify-between gap-2">
        <h3 className="text-[13px] font-medium text-muted-foreground">{title}</h3>
        <div className="flex items-center gap-1">
          {hasSecrets && !privacy.secrets ? (
            <Button size="xs" variant="ghost" aria-pressed={reveal} onClick={() => setReveal((r) => !r)}>
              {reveal ? <EyeSlashIcon data-icon="inline-start" /> : <EyeIcon data-icon="inline-start" />}
              {reveal ? "Hide secrets" : "Show secrets"}
            </Button>
          ) : null}
          <Button size="xs" variant="outline" onClick={() => void copy()}>
            <ClipboardTextIcon data-icon="inline-start" />
            Copy .env
          </Button>
        </div>
      </div>
      <pre className="overflow-x-auto rounded-lg bg-well p-3 font-mono text-xs leading-relaxed">
        {lines
          .map((line) => {
            const [key, ...rest] = line.split("=");
            const value = rest.join("=");
            return isSecretEnv(key, value) && !reveal ? `${key}=${"•".repeat(12)}` : privacy.text(line);
          })
          .join("\n")}
      </pre>
    </section>
  );
}
