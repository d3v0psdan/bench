import { createContext, useContext, type ReactNode } from "react";
import { useDaemon, type Daemon } from "@/hooks/use-daemon";

const DaemonContext = createContext<Daemon | null>(null);

/** DaemonProvider owns the single WS connection; panes consume it. */
export function DaemonProvider({ children }: { children: ReactNode }) {
  const daemon = useDaemon();
  return (
    <DaemonContext.Provider value={daemon}>{children}</DaemonContext.Provider>
  );
}

export function useDaemonContext(): Daemon {
  const ctx = useContext(DaemonContext);
  if (!ctx) throw new Error("useDaemonContext outside DaemonProvider");
  return ctx;
}
