import { Component, type ErrorInfo, type ReactNode } from "react";
import { getVersion } from "@tauri-apps/api/app";
import { BugIcon, ClipboardTextIcon, WarningCircleIcon } from "@phosphor-icons/react";
import { toast } from "sonner";
import { Alert, AlertAction, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { errorMessage } from "@/lib/async";
import { LINKS } from "@/lib/links";
import { openExternal } from "@/lib/open";

/** ErrorBoundary keeps a render crash inside the page that threw it, so the
 *  header and navigation stay usable instead of the window going blank.
 *  Remount it (change its key) to reset after navigating away. The crash
 *  can be copied for an issue (audit UI-39). */
export class ErrorBoundary extends Component<{ children: ReactNode }, { error: Error | null; where: string }> {
  state: { error: Error | null; where: string } = { error: null, where: "" };

  static getDerivedStateFromError(error: Error) {
    return { error };
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    console.error("render crash", error, info.componentStack);
    this.setState({ where: info.componentStack ?? "" });
  }

  copyReport = async () => {
    const { error, where } = this.state;
    const version = await getVersion().catch(() => "unknown");
    const details = [
      `Bench app ${version} on ${navigator.userAgent}`,
      "",
      `${error?.name}: ${error?.message}`,
      "",
      error?.stack ?? "",
      "",
      "Component stack:",
      where.trim(),
    ].join("\n");
    navigator.clipboard.writeText(details).then(
      () =>
        toast.success("Crash report copied", {
          action: { label: "Report an issue", onClick: () => openExternal(LINKS.issues) },
        }),
      (e: unknown) => toast.error("Couldn't copy", { description: errorMessage(e) }),
    );
  };

  render() {
    const { error } = this.state;
    if (!error) return this.props.children;
    return (
      <Alert variant="destructive">
        <WarningCircleIcon />
        <AlertTitle>This page crashed</AlertTitle>
        <AlertDescription className="font-mono text-xs break-words">{error.message}</AlertDescription>
        <AlertAction className="flex flex-wrap gap-2">
          <Button size="sm" variant="outline" onClick={() => void this.copyReport()}>
            <ClipboardTextIcon data-icon="inline-start" />
            Copy report
          </Button>
          <Button size="sm" variant="outline" onClick={() => openExternal(LINKS.issues)}>
            <BugIcon data-icon="inline-start" />
            Report an issue
          </Button>
          <Button size="sm" variant="outline" onClick={() => this.setState({ error: null, where: "" })}>
            Try again
          </Button>
        </AlertAction>
      </Alert>
    );
  }
}
