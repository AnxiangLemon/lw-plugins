import { useEffect, useMemo, useState } from "react";
import { CircleHelp, RefreshCw, Users } from "lucide-react";
import { useAppData } from "@/app/data-context";
import { api } from "@/lib/api";
import type { Dashboard, InviterStat } from "@/lib/types";
import { groupKey } from "@/lib/format";
import { Button } from "@/components/ui/button";
import { Avatar, AvatarFallback } from "@/components/ui/avatar";
import { Badge } from "@/components/ui/badge";
import { DataTable } from "@/components/shared/data-table";
import { PageHeader } from "@/components/shared/page-header";
import { SearchInput } from "@/components/shared/search-input";
import { EmptyState } from "@/components/shared/empty-state";
import { ErrorState } from "@/components/shared/error-state";
import { LoadingState } from "@/components/shared/loading-state";
import {
  IdentityCell,
  PeriodFilter,
  RoomFilter,
  roomFilterParams,
} from "./page-controls";

export function InvitersPage() {
  const { rooms, accountId, days, setDays, roomTitle, accountName, page } =
    useAppData();
  const [selectedRoom, setSelectedRoom] = useState("");
  const [query, setQuery] = useState("");
  const [data, setData] = useState<Dashboard | null>(null);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(true);
  const [revision, setRevision] = useState(0);
  const visibleRooms = rooms.filter(
    (room) => !accountId || room.account_id === accountId,
  );
  const room = visibleRooms.some((item) => groupKey(item) === selectedRoom)
    ? selectedRoom
    : "";

  useEffect(() => {
    if (page !== "inviters") return;
    let active = true;
    let inFlight = false;
    const controller = new AbortController();
    const params = new URLSearchParams({
      ...roomFilterParams(room, accountId),
      days: String(days),
    });
    async function load(initial: boolean) {
      if (inFlight) return;
      inFlight = true;
      if (initial) {
        setLoading(true);
        setData(null);
      }
      try {
        // 直接使用后端完整时间段聚合，不能拿历史列表的200条上限冒充邀请统计。
        const result = await api<Dashboard>(
          `/api/dashboard?${params}`,
          undefined,
          controller.signal,
        );
        if (active) {
          setData(result);
          setError("");
        }
      } catch (cause) {
        if (active)
          setError(cause instanceof Error ? cause.message : "加载邀请统计失败");
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
  }, [accountId, room, days, revision, page]);

  const ranks = useMemo(
    () =>
      new Map(
        (data?.inviters || []).map((item, index) => [
          `${groupKey(item)}\0${item.inviter_id}`,
          index + 1,
        ]),
      ),
    [data],
  );
  const rows = (data?.inviters || []).filter(
    (item) =>
      !query.trim() ||
      [item.name, item.inviter_id, roomTitle(item)].some((value) =>
        value.toLocaleLowerCase().includes(query.trim().toLocaleLowerCase()),
      ),
  );
  const period = days === 1 ? "今天" : `近 ${days} 天`;

  return (
    <div className="space-y-4">
      <PageHeader
        title="邀请人统计"
        description="看见每一份邀请贡献，来源以微信实际返回的信息为准。"
        actions={
          <Button
            variant="outline"
            loading={loading}
            onClick={() => setRevision((value) => value + 1)}
          >
            <RefreshCw className="size-4" />
            刷新数据
          </Button>
        }
      />
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex w-full flex-wrap items-center gap-3 lg:w-auto">
          <RoomFilter
            value={room}
            onChange={setSelectedRoom}
            rooms={visibleRooms}
            roomTitle={roomTitle}
            accountName={accountName}
          />
          <div className="w-full sm:w-64">
            <SearchInput
              value={query}
              onChange={setQuery}
              placeholder="搜索邀请人昵称或 ID"
            />
          </div>
        </div>
        <PeriodFilter days={days} onChange={setDays} />
      </div>
      {loading && !data ? (
        <LoadingState />
      ) : (
        <>
          {error && (
            <ErrorState
              message={error}
              onRetry={() => setRevision((value) => value + 1)}
            />
          )}
          {data && (
            <>
              <div className="grid gap-4 sm:grid-cols-3">
                <section className="rounded-lg border border-border bg-card p-4">
                  <div className="mb-2 flex items-center gap-2 text-sm text-muted-foreground">
                    <Users className="size-4 text-highlight" />
                    已知邀请来源
                  </div>
                  <div className="text-3xl font-semibold tracking-tight tabular-nums">
                    {data.known_invite_members.toLocaleString()}
                  </div>
                  <p className="mt-2 text-xs text-muted-foreground">
                    当前名单中，有邀请来源的成员
                  </p>
                </section>
                <section className="rounded-lg border border-border bg-card p-4">
                  <div className="mb-2 flex items-center gap-2 text-sm text-muted-foreground">
                    <CircleHelp className="size-4" />
                    来源未知
                  </div>
                  <div className="text-3xl font-semibold tracking-tight tabular-nums">
                    {data.unknown_invite_members.toLocaleString()}
                  </div>
                  <p className="mt-2 text-xs text-muted-foreground">
                    微信未返回邀请人的当前成员
                  </p>
                </section>
                <section className="rounded-lg border border-border bg-card p-4">
                  <div className="mb-2 text-sm text-muted-foreground">
                    {period} · 已确认邀请新增
                  </div>
                  <div className="text-3xl font-semibold tracking-tight text-highlight tabular-nums">
                    {data.inviters
                      .reduce((sum, item) => sum + item.joined, 0)
                      .toLocaleString()}
                  </div>
                  <p className="mt-2 text-xs text-muted-foreground">
                    记录期间新增，且可以确认邀请来源
                  </p>
                </section>
              </div>
              <div className="overflow-hidden rounded-lg border border-border bg-card">
                <div className="flex items-center justify-between gap-3 border-b border-border px-4 py-3">
                  <h2 className="text-base font-semibold">邀请贡献排行榜</h2>
                  <Badge variant="secondary">{period}</Badge>
                </div>
                <DataTable
                  rows={rows}
                  rowKey={(item: InviterStat) =>
                    `${groupKey(item)}\0${item.inviter_id}`
                  }
                  pageKey={`${accountId}|${room}|${days}|${query}`}
                  empty={
                    <EmptyState
                      compact
                      title={
                        query ? "没有找到这位邀请人" : "暂时没有可确认的邀请人"
                      }
                      description={
                        query
                          ? "尝试其他昵称或微信 ID。"
                          : "为群聊开启记录后，有邀请来源的成员会显示在这里。"
                      }
                    />
                  }
                  columns={[
                    {
                      key: "rank",
                      header: "排名",
                      className: "w-20",
                      cell: (item: InviterStat) => {
                        const rank =
                          ranks.get(`${groupKey(item)}\0${item.inviter_id}`) ||
                          0;
                        return (
                          <span
                            className={`font-mono text-sm ${rank <= 3 ? "text-highlight" : "text-muted-foreground"}`}
                          >
                            {String(rank).padStart(2, "0")}
                          </span>
                        );
                      },
                    },
                    {
                      key: "inviter",
                      header: "邀请人",
                      cell: (item: InviterStat) => (
                        <IdentityCell
                          title={item.name || item.inviter_id}
                          detail={item.inviter_id}
                          leading={
                            <Avatar className="size-9">
                              <AvatarFallback className="bg-secondary text-xs text-secondary-foreground">
                                {Array.from(item.name || item.inviter_id)
                                  .slice(0, 1)
                                  .join("")}
                              </AvatarFallback>
                            </Avatar>
                          }
                        />
                      ),
                    },
                    {
                      key: "room",
                      header: "所在群聊",
                      cell: (item: InviterStat) => (
                        <IdentityCell
                          title={roomTitle(item)}
                          detail={accountName(item.account_id)}
                        />
                      ),
                    },
                    {
                      key: "joined",
                      header: "期间新增",
                      cell: (item: InviterStat) => (
                        <span className="font-semibold text-highlight tabular-nums">
                          {item.joined.toLocaleString()}
                        </span>
                      ),
                    },
                    {
                      key: "members",
                      header: "当前成员",
                      cell: (item: InviterStat) => (
                        <span className="tabular-nums">
                          {item.current_members.toLocaleString()}
                        </span>
                      ),
                    },
                  ]}
                />
              </div>
              <p className="flex items-start gap-2 text-xs leading-6 text-muted-foreground">
                <CircleHelp className="mt-1 size-3.5 shrink-0" />
                <span>
                  扫码入群有邀请来源时同样计入。首次读取的已有成员只计入“当前成员”，不会记为“期间新增”；来源未知的成员不会猜测归属。
                </span>
              </p>
            </>
          )}
        </>
      )}
    </div>
  );
}
