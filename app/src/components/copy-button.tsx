import { useEffect, useRef, useState } from "react";
import { CheckIcon, CopyIcon } from "@phosphor-icons/react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { errorMessage } from "@/lib/async";

/** useCopied flips to true for a moment after a successful copy. */
export function useCopied(): [boolean, (text: string) => Promise<void>] {
  const [copied, setCopied] = useState(false);
  const timer = useRef<number | undefined>(undefined);
  useEffect(() => () => window.clearTimeout(timer.current), []);
  const copy = async (text: string) => {
    await navigator.clipboard.writeText(text);
    setCopied(true);
    window.clearTimeout(timer.current);
    timer.current = window.setTimeout(() => setCopied(false), 1500);
  };
  return [copied, copy];
}

export function CopyButton({ text, label }: { text: string; label: string }) {
  const [copied, copy] = useCopied();
  return (
    <Button
      size="icon-xs"
      variant="ghost"
      aria-label={copied ? `${label} copied` : `Copy ${label}`}
      onClick={() => copy(text).catch((e: unknown) => toast.error("Couldn't copy", { description: errorMessage(e) }))}
    >
      {copied ? <CheckIcon /> : <CopyIcon />}
    </Button>
  );
}
