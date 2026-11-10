import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from "react";
import { api, errorMessage, query } from "@/lib/api";
import { groupKey } from "@/lib/format";
import type {
  Account,
  Audit,
  Config,
  Dashboard,
  Operation,
  Page,
  Period,
  Room,
  StatusResponse,
} from "@/lib/types";

const pages: Page[] = [
  "dashboard",
  "groups",
  "inviters",
  "history",
  "operations",
  "settings",
];
function currentPage(): Page {
  const name = location.hash.slice(1) as Page;
  return pages.includes(name) ? name : "dashboard";
}
interface ScopedData {
  scope: string;
  dashboard: Dashboard | null;
  events: Audit[];
  operations: Operation[];
}
export interface Notice {
  id: number;
  message: string;
  type: "success" | "error";
}
interface AppData {
  status: StatusResponse | null;
  config: Config | null;
  accounts: Account[];
  rooms: Room[];
  operations: Operation[];
  events: Audit[];
  dashboard: Dashboard | null;
  accountId: string;
  setAccountId: (value: string) => void;
  days: Period;
  setDays: (value: Period) => void;
  page: Page;
  navigate: (page: Page) => void;
  loading: boolean;
  refreshing: boolean;
  error: string;
  refresh: () => Promise<void>;
  persist: (config: Config) => Promise<void>;
  notify: (message: string, type?: "success" | "error") => void;
  notice: Notice | null;
  dismissNotice: () => void;
  roomTitle: (room: Room) => string;
  accountName: (id: string) => string;
}
const DataContext = createContext<AppData | null>(null);

/** 服务端状态与页面编辑草稿分离；轮询只更新此处，绝不直接回填正在输入的表单。 */
export function AppDataProvider({ children }: { children: ReactNode }) {
  const [status, setStatus] = useState<StatusResponse | null>(null);
  const [accounts, setAccounts] = useState<Account[]>([]);
  const [directory, setDirectory] = useState<Room[]>([]);
  const [accountId, setAccountId] = useState("");
  const [days, setDays] = useState<Period>(7);
  const [page, setPage] = useState<Page>(currentPage);
  const [data, setData] = useState<ScopedData>({
    scope: "",
    dashboard: null,
    events: [],
    operations: [],
  });
  const [refreshing, setRefreshing] = useState(false);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState<Notice | null>(null);
  const requestVersion = useRef(0);
  const pending = useRef<AbortController | null>(null);
  const saving = useRef(false);
  const scope = `${accountId}:${days}`;
  const config = status?.config || null;
  const notify = useCallback(
    (message: string, type: "success" | "error" = "success") =>
      setNotice({ id: Date.now(), message, type }),
    [],
  );
  useEffect(() => {
    if (!notice) return;
    const timer = window.setTimeout(() => setNotice(null), 6500);
    return () => window.clearTimeout(timer);
  }, [notice]);
  const refresh = useCallback(async () => {
    const version = ++requestVersion.current;
    pending.current?.abort();
    const controller = new AbortController();
    pending.current = controller;
    setRefreshing(true);
    try {
      const nextStatus = await api<StatusResponse>(
        "/api/status",
        undefined,
        controller.signal,
      );
      const params = query({ account_id: accountId, days });
      const results = await Promise.allSettled([
        api<Room[]>("/api/rooms", undefined, controller.signal),
        api<Dashboard>(
          `/api/dashboard?${params}`,
          undefined,
          controller.signal,
        ),
        api<Audit[]>(
          `/api/events?${query({ account_id: accountId, limit: 5 })}`,
          undefined,
          controller.signal,
        ),
        api<Operation[]>(
          `/api/operations?${query({ account_id: accountId })}`,
          undefined,
          controller.signal,
        ),
        nextStatus.connected
          ? api<Account[]>("/api/accounts", undefined, controller.signal)
          : Promise.resolve(null),
      ]);
      if (version !== requestVersion.current) return;
      setStatus(nextStatus);
      const [
        roomsResult,
        dashboardResult,
        eventsResult,
        operationsResult,
        accountsResult,
      ] = results;
      if (roomsResult.status === "fulfilled")
        setDirectory(roomsResult.value || []);
      if (accountsResult.status === "fulfilled" && accountsResult.value)
        setAccounts(accountsResult.value);
      setData((previous) => ({
        scope,
        dashboard:
          dashboardResult.status === "fulfilled"
            ? dashboardResult.value
            : previous.scope === scope
              ? previous.dashboard
              : null,
        events:
          eventsResult.status === "fulfilled"
            ? eventsResult.value || []
            : previous.scope === scope
              ? previous.events
              : [],
        operations:
          operationsResult.status === "fulfilled"
            ? operationsResult.value || []
            : previous.scope === scope
              ? previous.operations
              : [],
      }));
      const failure = results.find((result) => result.status === "rejected");
      setError(
        failure?.status === "rejected" ? errorMessage(failure.reason) : "",
      );
    } catch (cause) {
      if (!controller.signal.aborted && version === requestVersion.current)
        setError(errorMessage(cause));
    } finally {
      if (version === requestVersion.current) {
        pending.current = null;
        setRefreshing(false);
      }
    }
  }, [accountId, days, scope]);
  useEffect(() => {
    void refresh();
    // 隐藏标签页或正在进行配置保存时暂停自动刷新，减少无意义请求和竞态。
    const timer = window.setInterval(() => {
      if (!document.hidden && !saving.current && !pending.current)
        void refresh();
    }, 5000);
    // 此处读取的是请求计数器（不是 DOM ref）；失效函数在清理时使旧响应失效。
    const invalidate = () => {
      requestVersion.current += 1;
      pending.current?.abort();
    };
    return () => {
      window.clearInterval(timer);
      invalidate();
    };
  }, [refresh]);
  const persist = useCallback(
    async (next: Config) => {
      if (saving.current) throw new Error("设置正在保存，请稍候");
      saving.current = true;
      try {
        await api("/api/config", next);
        setStatus((previous) =>
          previous ? { ...previous, config: next } : previous,
        );
        notify("设置已保存");
        await refresh();
      } finally {
        saving.current = false;
      }
    },
    [notify, refresh],
  );
  const navigate = useCallback((next: Page) => {
    if (location.hash !== `#${next}`) history.pushState(null, "", `#${next}`);
    setPage(next);
    window.scrollTo({ top: 0, behavior: "instant" });
  }, []);
  useEffect(() => {
    const change = () => {
      setPage(currentPage());
      window.scrollTo({ top: 0, behavior: "instant" });
    };
    window.addEventListener("hashchange", change);
    window.addEventListener("popstate", change);
    return () => {
      window.removeEventListener("hashchange", change);
      window.removeEventListener("popstate", change);
    };
  }, []);
  const rooms = useMemo(() => {
    const merged = new Map(directory.map((room) => [groupKey(room), room]));
    for (const group of config?.groups || [])
      if (!merged.has(groupKey(group)))
        merged.set(groupKey(group), { ...group, name: group.label });
    return [...merged.values()].sort((a, b) =>
      (a.remark || a.name || a.room_id).localeCompare(
        b.remark || b.name || b.room_id,
        "zh-CN",
      ),
    );
  }, [directory, config?.groups]);
  const availableAccounts = useMemo(() => {
    const merged = new Map(
      accounts.map((account) => [account.account_id, account]),
    );
    for (const room of rooms)
      if (!merged.has(room.account_id))
        merged.set(room.account_id, {
          account_id: room.account_id,
          display_name: room.account_id,
          online: false,
        });
    return [...merged.values()];
  }, [accounts, rooms]);
  const roomTitle = useCallback(
    (room: Room) => {
      const actual = rooms.find((item) => groupKey(item) === groupKey(room));
      return (
        actual?.remark ||
        actual?.name ||
        room.label ||
        room.name ||
        room.room_id
      );
    },
    [rooms],
  );
  const accountName = useCallback(
    (id: string) =>
      availableAccounts.find((account) => account.account_id === id)
        ?.display_name || id,
    [availableAccounts],
  );
  const value: AppData = {
    status,
    config,
    accounts: availableAccounts,
    rooms,
    accountId,
    setAccountId,
    days,
    setDays,
    operations: data.scope === scope ? data.operations : [],
    events: data.scope === scope ? data.events : [],
    dashboard: data.scope === scope ? data.dashboard : null,
    page,
    navigate,
    loading: !status || data.scope !== scope,
    refreshing,
    error,
    refresh,
    persist,
    notify,
    notice,
    dismissNotice: () => setNotice(null),
    roomTitle,
    accountName,
  };
  return <DataContext.Provider value={value}>{children}</DataContext.Provider>;
}
export function useAppData() {
  const context = useContext(DataContext);
  if (!context) throw new Error("useAppData 必须放在 AppDataProvider 内");
  return context;
}
