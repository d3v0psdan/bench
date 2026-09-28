import { cn } from "@/lib/utils"

function Empty({ className, ...props }: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="empty"
      className={cn(
        "group/empty flex w-full min-w-0 flex-1 flex-col items-center justify-center gap-4 rounded-lg border-dashed p-12 text-center text-balance",
        className
      )}
      {...props}
    />
  )
}

function EmptyHeader({ className, ...props }: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="empty-header"
      className={cn("flex max-w-sm flex-col items-center gap-2", className)}
      {...props}
    />
  )
}

// The fanned cards behind the icon, back to front: offset up and right like
// a stack of files, spreading a little further while the empty state is hovered.
const STACK_CARDS = [
  "translate-x-4 -translate-y-4 bg-well opacity-60 group-hover/empty:translate-x-6 group-hover/empty:-translate-y-6",
  "translate-x-2 -translate-y-2 bg-well opacity-85 group-hover/empty:translate-x-3 group-hover/empty:-translate-y-3",
]

function EmptyMedia({
  className,
  variant = "default",
  children,
  ...props
}: React.ComponentProps<"div"> & { variant?: "default" | "icon" }) {
  if (variant === "icon") {
    return (
      <div
        data-slot="empty-icon"
        data-variant={variant}
        className={cn("relative mb-2 flex size-28 shrink-0 items-center justify-center perspective-[600px]", className)}
        {...props}
      >
        <span aria-hidden className="absolute inset-0 bg-dot-grid [mask-image:radial-gradient(closest-side,black,transparent)]" />
        <div className="relative size-12 rotate-x-[24deg] -rotate-z-[10deg] transform-3d">
          {STACK_CARDS.map((card) => (
            <span
              key={card}
              aria-hidden
              className={cn(
                "absolute inset-0 rounded-xl shadow-card transition-[translate] duration-300 ease-out motion-reduce:transition-none",
                card
              )}
            />
          ))}
          <div className="relative flex size-full items-center justify-center rounded-xl bg-card text-foreground shadow-popover transition-[translate] duration-300 ease-out group-hover/empty:-translate-y-1 motion-reduce:transition-none [&_svg]:pointer-events-none [&_svg]:shrink-0 [&_svg:not([class*='size-'])]:size-6">
            {children}
          </div>
        </div>
      </div>
    )
  }
  return (
    <div
      data-slot="empty-icon"
      data-variant={variant}
      className={cn("mb-2 flex shrink-0 items-center justify-center [&_svg]:pointer-events-none [&_svg]:shrink-0", className)}
      {...props}
    >
      {children}
    </div>
  )
}

function EmptyTitle({ className, ...props }: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="empty-title"
      className={cn(
        "font-heading text-lg font-medium tracking-tight",
        className
      )}
      {...props}
    />
  )
}

function EmptyDescription({ className, ...props }: React.ComponentProps<"p">) {
  return (
    <div
      data-slot="empty-description"
      className={cn(
        "text-sm/relaxed text-muted-foreground [&>a]:underline [&>a]:underline-offset-4 [&>a:hover]:text-primary",
        className
      )}
      {...props}
    />
  )
}

function EmptyContent({ className, ...props }: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="empty-content"
      className={cn(
        "flex w-full max-w-sm min-w-0 flex-col items-center gap-4 text-sm text-balance",
        className
      )}
      {...props}
    />
  )
}

export {
  Empty,
  EmptyHeader,
  EmptyTitle,
  EmptyDescription,
  EmptyContent,
  EmptyMedia,
}
