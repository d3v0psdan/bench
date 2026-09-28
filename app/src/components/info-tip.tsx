import type { ReactNode } from "react";
import { InfoIcon } from "@phosphor-icons/react";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";

/** InfoTip keeps a secondary fact out of the flow: a small info icon that
 *  shows one or two short sentences on hover or keyboard focus. For more
 *  than that, use LearnMore. */
export function InfoTip({ label, children, className }: { label: string; children: ReactNode; className?: string }) {
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <button type="button" aria-label={label} className={infoButton(className)}>
          <InfoIcon className="size-3.5" />
        </button>
      </TooltipTrigger>
      <TooltipContent className="max-w-64 text-balance">{children}</TooltipContent>
    </Tooltip>
  );
}

/** LearnMore is InfoTip for longer help: the icon opens a small panel with
 *  a title and a few paragraphs or a list, and stays open while it's read. */
export function LearnMore({
  title,
  children,
  className,
}: {
  title: string;
  children: ReactNode;
  className?: string;
}) {
  return (
    <Popover>
      <PopoverTrigger asChild>
        <button type="button" aria-label={`Learn more: ${title}`} className={infoButton(className)}>
          <InfoIcon className="size-3.5" />
        </button>
      </PopoverTrigger>
      <PopoverContent className="w-80 text-[13px]">
        <p className="mb-1.5 font-medium">{title}</p>
        <div className="flex flex-col gap-2 text-muted-foreground">{children}</div>
      </PopoverContent>
    </Popover>
  );
}

/** infoButton styles the round info icon button, for triggers built elsewhere
 *  (the Sites list's side peek). */
export function infoButton(className?: string) {
  return cn(
    "inline-flex size-5 shrink-0 items-center justify-center rounded-full align-middle text-muted-foreground outline-none transition-colors duration-100 hover:text-foreground focus-visible:ring-3 focus-visible:ring-ring",
    className,
  );
}
