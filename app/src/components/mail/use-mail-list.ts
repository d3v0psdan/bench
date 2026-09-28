import { useEffect, useState } from "react";
import { useDaemonContext } from "@/hooks/daemon-provider";
import { useRoute } from "@/hooks/route";
import { api, type Conn, type MailList } from "@/lib/api";
import { DEFAULT_TIMEOUT_MS, errorMessage, withTimeout } from "@/lib/async";

export const ALL = "__all__";
const PAGE_SIZE = 100;
const SEARCH_DEBOUNCE_MS = 250;

/** useMailList is the inbox list the Mail page shows: which inbox and
 *  search, how many pages are loaded, and the inboxes Mailpit knows. It
 *  refetches on every relayed Mailpit event. */
export function useMailList(conn: Conn | null) {
  const { mailVersion } = useDaemonContext();
  const { route } = useRoute();
  // A site's Mail card opens its app's inbox straight away.
  const routeInbox = route.kind === "home" ? route.inbox : undefined;
  const [inbox, setInbox] = useState(routeInbox ?? ALL);
  useEffect(() => {
    if (routeInbox) setInbox(routeInbox);
  }, [routeInbox]);
  const [list, setList] = useState<MailList | null>(null);
  const [listError, setListError] = useState<string | null>(null);
  const [retry, setRetry] = useState(0);
  const [query, setQuery] = useState("");
  const [search, setSearch] = useState(""); // query, once typing pauses
  const [limit, setLimit] = useState(PAGE_SIZE);
  const [fetching, setFetching] = useState(false);

  useEffect(() => {
    const t = window.setTimeout(() => setSearch(query.trim()), SEARCH_DEBOUNCE_MS);
    return () => window.clearTimeout(t);
  }, [query]);
  // A new filter starts from the first page again.
  useEffect(() => setLimit(PAGE_SIZE), [inbox, search]);

  // Refetch on filter change and on every relayed Mailpit event.
  useEffect(() => {
    if (!conn) return;
    let stale = false;
    const terms = [inbox === ALL ? "" : `tag:"${inbox}"`, search].filter(Boolean).join(" ");
    const path = terms
      ? `/api/mail/v1/search?limit=${limit}&query=${encodeURIComponent(terms)}`
      : `/api/mail/v1/messages?limit=${limit}`;
    setFetching(true);
    withTimeout(api<MailList>(conn, "GET", path), DEFAULT_TIMEOUT_MS, "Mail didn't answer in time")
      .then((l) => {
        if (stale) return;
        setList(l);
        setListError(null);
      })
      .catch((e) => {
        if (!stale) setListError(errorMessage(e));
      })
      .finally(() => {
        if (!stale) setFetching(false);
      });
    return () => {
      stale = true;
    };
  }, [conn, inbox, search, limit, mailVersion, retry]);

  // Every Mailpit list and search answers with the mailbox-wide tags, so
  // they are read from each response (a copy kept in state goes stale).
  const tags = list?.tags ?? [];

  return {
    inbox,
    setInbox,
    list,
    listError,
    retry: () => setRetry((n) => n + 1),
    query,
    setQuery,
    search,
    loadMore: () => setLimit((l) => l + PAGE_SIZE),
    fetching,
    tags,
  };
}
