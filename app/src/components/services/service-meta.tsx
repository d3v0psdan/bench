import {
  CubeIcon,
  DatabaseIcon,
  EnvelopeSimpleIcon,
  type Icon,
  LightningIcon,
  MagnifyingGlassIcon,
} from "@phosphor-icons/react";
import { StatusDot } from "@/components/page";
import type { Service } from "@/lib/api";

/** Display names for the catalog kinds (the API also sends labels with
 *  the catalog; rows only have the kind). */
export const SERVICE_LABELS: Record<string, string> = {
  mysql: "MySQL",
  mariadb: "MariaDB",
  postgresql: "PostgreSQL",
  valkey: "Valkey",
  meilisearch: "Meilisearch",
  rustfs: "RustFS",
  mailpit: "Mail",
};

export const SERVICE_ICONS: Record<string, Icon> = {
  mysql: DatabaseIcon,
  mariadb: DatabaseIcon,
  postgresql: DatabaseIcon,
  valkey: LightningIcon,
  meilisearch: MagnifyingGlassIcon,
  rustfs: CubeIcon,
  mailpit: EnvelopeSimpleIcon,
};

export const TRANSITIONAL = new Set(["starting", "installing", "initializing", "cloning", "stopping", "deleting", "backoff"]);

/** ServiceStateDot renders a supervisor state or lifecycle phase. */
export function ServiceStateDot({ state }: { state: string }) {
  if (state === "running") return <StatusDot tone="success">Running</StatusDot>;
  if (state === "failed") return <StatusDot tone="danger">Failed</StatusDot>;
  if (TRANSITIONAL.has(state)) {
    const label = state === "backoff" ? "Restarting" : state[0].toUpperCase() + state.slice(1);
    return (
      <StatusDot tone="warning" busy>
        {label}
      </StatusDot>
    );
  }
  return <StatusDot tone="neutral">Stopped</StatusDot>;
}

/** serviceAddress is where a service listens. A new instance has no port
 *  until its first start picks one. */
export function serviceAddress(s: Service) {
  return s.port ? `127.0.0.1:${s.port}` : "port picked on first start";
}
