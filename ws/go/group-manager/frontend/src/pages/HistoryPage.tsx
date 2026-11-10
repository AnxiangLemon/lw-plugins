import { useEffect, useState } from "react";
import { Download, Info, RefreshCw } from "lucide-react";
import { useAppData } from "@/app/data-context";
import { api } from "@/lib/api";
import { formatTime, groupKey, statuses } from "@/lib/format";
import type { Audit } from "@/lib/types";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { DataTable } from "@/components/shared/data-table";
import { PageHeader } from "@/components/shared/page-header";
import { SearchInput } from "@/components/shared/search-input";
import { EmptyState } from "@/components/shared/empty-state";
import { ErrorState } from "@/components/shared/error-state";
import { LoadingState } from "@/components/shared/loading-state";
import { IdentityCell, RoomFilter, roomFilterParams } from "./page-controls";

export function HistoryPage() {
  const { rooms, accountId, roomTitle, accountName, notify, page } =
    useAppData();
  const [selectedRoom, setSelectedRoom] = useState("");
  const [kind, setKind] = useState("");
  const [query, setQuery] = useState("");
  const [search, setSearch] = useState("");
  const [rows, setRows] = useState<Audit[] | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [revision, setRevision] = useState(0);
  const [pending, setPending] = useState<number | null>(null);
  const visibleRooms = rooms.filter(
    (room) => !accountId || room.account_id === accountId,
  );
  const room = visibleRooms.some((item) => groupKey(item) === selectedRoom)
    ? selectedRoom
    : "";
  // 搜索输入只改变查询条件；使用同一参数生成列表请求与CSV下载，确保导出对应当前筛选。
  useEffect(() => {
    const timer = window.setTimeout(() => setSearch(query.trim()), 300);
    return () => window.clearTimeout(timer);
  }, [query]);
  const params = new URLSearchParams({
    ...roomFilterParams(room, accountId),
    q: search,
    kind,
    limit: "200",
  }).toString();

  useEffect(() => {
    if (page !== "history") return;
    let active = true;
    let inFlight = false;
    const controller = new AbortController();
    async function load(initial: boolean) {
      if (inFlight) return;
      inFlight = true;
      if (initial) {
        setLoading(true);
        setRows(null);
      }
      try {
        const result = await api<Audit[] | null>(
          `/api/events?${params}`,
          undefined,
          controller.signal,
        );
        if (active) {
          setRows(result || []);
          setError("");
        }
      } catch (cause) {
        if (active)
          setError(cause instanceof Error ? cause.message : "加载成员动态失败");
      } finally {
        inFlight = false;
        if (active) setLoading(false);
      }
    }
    void load(true);
    const timer = window.setInterval(() => {
      if (!document.hidden) void load(false);
    }, 5000);
    return () => {
      active = false;
      window.clearInterval(timer);
      controller.abort();
    };
  }, [params, revision, page]);

  async function receipt(item: Audit) {
    if (pending !== null) return;
    setPending(item.id);
    try {
      // 查询原通知回执，不生成新操作，不重新发送欢迎或离群消息。
      await api("/api/receipt", { id: item.id });
      setRevision((value) => value + 1);
      notify("已查询原消息结果");
    } catch (cause) {
      notify(
        cause instanceof Error ? cause.message : "查询消息结果失败",
        "error",
      );
    } finally {
      setPending(null);
    }
  }

  return (
    <div className="space-y-4">
      <PageHeader
        title="成员动态"
        description="入群与离群持续留档，让每一次成员变化都有迹可循。"
        actions={
          <>
            <Button
              variant="outline"
              loading={loading}
              onClick={() => setRevision((value) => value + 1)}
            >
              <RefreshCw className="size-4" />
              刷新
            </Button>
            <Button variant="outline" asChild>
              <a href={`/api/events.csv?${params}`} download>
                <Download className="size-4" />
                导出 CSV
              </a>
            </Button>
          </>
        }
      />
      <div className="flex flex-wrap items-center gap-3">
        <RoomFilter
          value={room}
          onChange={setSelectedRoom}
          rooms={visibleRooms}
          roomTitle={roomTitle}
          accountName={accountName}
        />
        <Select
          value={kind || "all"}
          onValueChange={(value) => setKind(value === "all" ? "" : value)}
        >
          <SelectTrigger aria-label="按动态筛选" className="w-full sm:w-40">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">全部动态</SelectItem>
            <SelectItem value="joined">成员入群</SelectItem>
            <SelectItem value="left">成员离群</SelectItem>
            <SelectItem value="baseline">开始记录</SelectItem>
          </SelectContent>
        </Select>
        <div className="w-full sm:max-w-xs sm:flex-1">
          <SearchInput
            value={query}
            onChange={setQuery}
            placeholder="搜索成员昵称或 ID"
          />
        </div>
      </div>
      {loading && !rows ? (
        <LoadingState />
      ) : (
        <>
          {error && (
            <ErrorState
              message={error}
              onRetry={() => setRevision((value) => value + 1)}
            />
          )}
          {rows && (
            <div className="overflow-hidden rounded-lg border border-border bg-card">
              <DataTable
                rows={rows}
                rowKey={(item: Audit) => String(item.id)}
                pageKey={params}
                empty={
                  <EmptyState
                    compact
                    title={
                      search || kind || room
                        ? "没有符合条件的动态"
                        : "暂无成员动态"
                    }
                    description={
                      search || kind || room
                        ? "调整群聊、动态类型或搜索关键词后再试。"
                        : "首次加载只保存当前名单，后续发现入群、离群时会自动记录。"
                    }
                  />
                }
                columns={[
                  {
                    key: "member",
                    header: "成员 / 记录",
                    cell: (item: Audit) => (
                      <IdentityCell
                        title={
                          item.kind === "baseline"
                            ? "已开始记录"
                            : item.name || item.member_id
                        }
                        detail={
                          item.kind === "baseline"
                            ? `当前 ${item.after_count} 位成员`
                            : item.member_id
                        }
                      />
                    ),
                  },
                  {
                    key: "room",
                    header: "群聊",
                    cell: (item: Audit) => (
                      <IdentityCell
                        title={roomTitle(item)}
                        detail={accountName(item.account_id)}
                      />
                    ),
                  },
                  {
                    key: "kind",
                    header: "动态",
                    cell: (item: Audit) => (
                      <Badge
                        variant={
                          item.kind === "joined"
                            ? "success"
                            : item.kind === "left"
                              ? "warning"
                              : "secondary"
                        }
                      >
                        {item.kind === "joined"
                          ? "成员入群"
                          : item.kind === "left"
                            ? "成员离群"
                            : item.kind === "baseline"
                              ? "开始记录"
                              : item.kind}
                      </Badge>
                    ),
                  },
                  {
                    key: "inviter",
                    header: "邀请来源",
                    cell: (item: Audit) =>
                      item.inviter_id ? (
                        <IdentityCell
                          title={item.inviter_name || item.inviter_id}
                          detail={item.inviter_id}
                        />
                      ) : (
                        <span className="text-xs text-muted-foreground">
                          来源未知
                        </span>
                      ),
                  },
                  {
                    key: "time",
                    header: "发现时间",
                    className: "whitespace-nowrap",
                    cell: (item: Audit) => (
                      <div>
                        <span className="text-sm tabular-nums">
                          {formatTime(item.observed_at)}
                        </span>
                        {item.gap && (
                          <div className="mt-1 text-xs text-warning">
                            离线后补记
                          </div>
                        )}
                      </div>
                    ),
                  },
                  {
                    key: "notification",
                    header: "消息提醒",
                    cell: (item: Audit) => (
                      <div className="flex flex-col items-start gap-1">
                        <span
                          className="text-xs text-muted-foreground"
                          title={item.notify_error}
                        >
                          {statuses[item.notify_status] ||
                            item.notify_status ||
                            "仅记录"}
                        </span>
                        {["unknown", "queued", "running"].includes(
                          item.notify_status,
                        ) && (
                          <Button
                            variant="ghost"
                            size="sm"
                            className="h-8 px-0 text-sm"
                            loading={pending === item.id}
                            disabled={pending !== null}
                            onClick={() => void receipt(item)}
                          >
                            查询原结果
                          </Button>
                        )}
                      </div>
                    ),
                  },
                ]}
              />
            </div>
          )}
        </>
      )}
      <p className="flex items-start gap-2 text-xs leading-6 text-muted-foreground">
        <Info className="mt-1 size-3.5 shrink-0" />
        <span>
          显示最近 200
          条匹配记录，分页仅对这些记录生效。时间为发现时间；离线后补记会单独标明。名单变化无法区分主动退群或被移出。
        </span>
      </p>
    </div>
  );
}
