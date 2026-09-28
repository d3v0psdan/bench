import { useTheme } from "next-themes"
import { Toaster as Sonner, type ToasterProps } from "sonner"
import { CheckCircleIcon, InfoIcon, WarningIcon, XCircleIcon, SpinnerIcon } from "@phosphor-icons/react"

// Toasts are a dark pill ("Changes saved"), in both themes.
const Toaster = ({ ...props }: ToasterProps) => {
  const { resolvedTheme = "light" } = useTheme()

  return (
    <Sonner
      theme={resolvedTheme as ToasterProps["theme"]}
      className="toaster group"
      icons={{
        success: (
          <CheckCircleIcon weight="fill" className="size-4 text-success-solid" />
        ),
        info: (
          <InfoIcon weight="fill" className="size-4 text-brand" />
        ),
        warning: (
          <WarningIcon weight="fill" className="size-4 text-warning-solid" />
        ),
        error: (
          <XCircleIcon weight="fill" className="size-4 text-destructive-solid" />
        ),
        loading: (
          <SpinnerIcon className="size-4 animate-spin" />
        ),
      }}
      style={
        {
          "--normal-bg": "var(--pill)",
          "--normal-text": "var(--pill-foreground)",
          "--normal-border": "var(--pill)",
          "--border-radius": "calc(var(--radius) * 1.25)",
        } as React.CSSProperties
      }
      toastOptions={{
        classNames: {
          // The pill hugs its content and centers in the (wider) toast
          // list; a min width left short messages sitting off-center.
          toast: "cn-toast !inset-x-0 !mx-auto !w-fit !max-w-full !px-4 !py-3 !text-[13px] !shadow-popover",
          // The pill is dark in both themes, but Sonner colors descriptions
          // for its own light theme (#3f3f3f), unreadable on the pill.
          description: "!text-pill-muted",
        },
      }}
      {...props}
    />
  )
}

export { Toaster }
