import { lazy, Suspense } from "react";
import {
  ArrowDownRight,
  ArrowRight,
  ArrowUpRight,
  ChartNoAxesCombined,
  Check,
  Circle,
  RefreshCw,
  UserRoundPlus,
  UsersRound,
} from "lucide-react";
import { useAppData } from "@/app/data-context";
import { formatNumber, formatTime, kinds } from "@/lib/format";
import type { Audit } from "@/lib/types";
import { Button } from "@/components/ui/button";
import { Avatar, AvatarFallback } from "@/components/ui/avatar";
import { Badge } from "@/components/ui/badge";
import { PageHeader } from "@/components/shared/page-header";
import { DataTable } from "@/components/shared/data-table";
import { EmptyState } from "@/components/shared/empty-state";
import { ErrorState } from "@/components/shared/error-state";
import { LoadingState } from "@/components/shared/loading-state";
import { PeriodFilter, IdentityCell } from "./page-controls";
const TrendChart = lazy(() => import("./TrendChart"));

export function DashboardPage() {
  const {
    page,
    status,
    config,
    dashboard,
    events,
    rooms,
    accountId,
    days,
    setDays,
    loading,
    error,
    refreshing,
    refresh,
    navigate,
    roomTitle,
    accountName,
  } = useAppData();
  if (error && !status)
    return <ErrorState message={error} onRetry={() => void refresh()} />;
  const visibleRooms = rooms.filter(
    (room) => !accountId || room.account_id === accountId,
  );
  const tracked =
    config?.groups.filter(
      (group) =>
        group.enabled && (!accountId || group.account_id === accountId),
    ).length || 0;
  const period = days === 1 ? "今天" : `近 ${days} 天`;
  const number = (value?: number) =>
    value === undefined || loading ? "—" : formatNumber(value);
  const hasChanges = dashboard?.trend?.some(
    (point) => point.joined || point.left,
  );
  // 无变化时缩短图表区域；数据仍来自原接口，不用占位曲线制造增长。
  const chartHeight = hasChanges ? "h-[208px]" : "h-[168px]";
  const steps = [
    { text: "连接 Desktop", done: !!status?.connected },
    { text: "同步微信群聊", done: visibleRooms.length > 0 },
    { text: "开启成员记录", done: tracked > 0 },
  ];
  return (
    <>
      <PageHeader
        title="数据概览"
        description="每一份增长，每一次变化，都有迹可循。"
        actions={
          <Button
            variant="outline"
            size="sm"
            loading={refreshing}
            onClick={() => void refresh()}
          >
            <RefreshCw className="size-3.5" />
            刷新数据
          </Button>
        }
      />
      <div className="mb-4 flex flex-wrap items-center justify-between gap-3">
        <div className="flex items-center gap-2">
          <span className="text-sm font-medium">社群运营概况</span>
          <span className="hidden text-xs text-muted-foreground sm:inline">
            {new Date().toLocaleDateString("zh-CN", {
              month: "long",
              day: "numeric",
              weekday: "long",
            })}
          </span>
        </div>
        <PeriodFilter days={days} onChange={setDays} />
      </div>
      <section
        aria-label="运营统计"
        className="mb-4 grid grid-cols-2 gap-3 xl:grid-cols-4 xl:gap-4"
      >
        {[
          {
            title: "正在记录的群",
            value: number(config ? tracked : undefined),
            detail: `已同步 ${visibleRooms.length} 个群聊`,
            note: "记录覆盖",
            icon: UsersRound,
          },
          {
            title: "已记录成员",
            value: number(dashboard?.members),
            detail: "各群当前名单中的成员合计",
            note: "最新名单",
            icon: UsersRound,
          },
          {
            title: "新增成员",
            value: number(dashboard?.joined),
            detail: `${period}发现的成员加入`,
            note: "入群动态",
            icon: ArrowUpRight,
          },
          {
            title: "离群成员",
            value: number(dashboard?.left),
            detail: `${period}发现的成员离开`,
            note: "离群动态",
            icon: ArrowDownRight,
          },
        ].map((stat) => (
          <article
            key={stat.title}
            className="rounded-lg border border-border/65 bg-card p-4"
          >
            <div className="flex items-center justify-between gap-2">
              <h2 className="text-sm font-medium text-muted-foreground">{stat.title}</h2>
              <stat.icon
                className="size-4 text-highlight/80"
                strokeWidth={1.5}
              />
            </div>
            <div className="mt-2 flex items-end justify-between gap-2">
              <strong className="text-[30px] font-semibold leading-none tracking-tight tabular-nums sm:text-[34px]">
                {stat.value}
              </strong>
              <span className="text-xs text-muted-foreground">
                {stat.note}
              </span>
            </div>
            <p className="mt-2 text-xs text-muted-foreground">
              {stat.detail}
            </p>
          </article>
        ))}
      </section>
      <div className="mb-4 grid gap-4 xl:grid-cols-[minmax(0,1.8fr)_minmax(300px,1fr)]">
        <section className="min-w-0 rounded-lg border border-border/65 bg-card">
          <div className="flex flex-wrap items-center justify-between gap-2 px-4 pb-2 pt-4">
            <div>
              <h2 className="text-base font-semibold">成员变化趋势</h2>
              <p className="mt-1 text-xs text-muted-foreground">
                {period} · 按发现日期统计
              </p>
            </div>
            <div className="flex gap-4 text-xs text-muted-foreground">
              <span className="flex items-center gap-1.5">
                <span className="h-0.5 w-3 bg-chart-join" />
                新增
              </span>
              <span className="flex items-center gap-1.5">
                <span className="h-0.5 w-3 bg-chart-left" />
                离群
              </span>
            </div>
          </div>
          {loading ? (
            <LoadingState className="h-[168px] min-h-0" />
          ) : (
            <div className={`group/chart relative ${chartHeight}`}>
              {page === "dashboard" && (
                <Suspense fallback={<LoadingState className="h-[168px] min-h-0" />}>
                  <TrendChart points={dashboard?.trend || []} />
                </Suspense>
              )}
              {!hasChanges && (
                <div className="pointer-events-none absolute inset-0 flex flex-col items-center justify-center px-4 text-center transition-opacity group-hover/chart:opacity-0">
                  <div className="flex flex-col items-center rounded-lg bg-card/95 px-4 py-2">
                    <div className="mb-2 flex size-8 items-center justify-center rounded-md bg-chart-join/10">
                      <ChartNoAxesCombined className="size-4 text-chart-join" />
                    </div>
                    <h3 className="text-sm font-medium">
                      {tracked
                        ? "这段时间还没有成员变化"
                        : "从第一条成员记录开始"}
                    </h3>
                    <p className="mt-1 max-w-sm text-xs text-muted-foreground">
                      {tracked
                        ? "检测到入群或离群后，趋势将自动显示在这里。"
                        : "为群聊开启记录，之后发生的入群与离群会自动汇总。"}
                    </p>
                  </div>
                </div>
              )}
            </div>
          )}
          <div className="flex flex-wrap justify-between gap-1 border-t border-border/60 px-4 py-2.5 text-xs text-muted-foreground">
            <span>首次读取已有名单不会计入新增</span>
            {dashboard && (
              <span>
                净变化{" "}
                <strong className="ml-1 font-medium text-foreground">
                  {dashboard.joined - dashboard.left > 0 ? "+" : ""}
                  {dashboard.joined - dashboard.left}
                </strong>{" "}
                人
              </span>
            )}
          </div>
        </section>
        <section className="flex min-w-0 flex-col rounded-lg border border-border/65 bg-card">
          <div className="flex items-start justify-between gap-2 px-4 pb-2 pt-4">
            <div>
              <h2 className="text-base font-semibold">邀请贡献榜</h2>
              <p className="mt-1 text-xs text-muted-foreground">
                {period} · 微信返回的邀请来源
              </p>
            </div>
            <Button
              variant="ghost"
              size="icon-sm"
              aria-label="查看全部邀请人"
              onClick={() => navigate("inviters")}
            >
              <ArrowUpRight className="size-3.5" />
            </Button>
          </div>
          {loading ? (
            <LoadingState className="min-h-36 flex-1 py-4" />
          ) : dashboard?.inviters?.length ? (
            <div className="flex-1 px-4 pb-2">
              {dashboard.inviters.slice(0, 4).map((person, index) => (
                <div
                  key={`${person.account_id}:${person.room_id}:${person.inviter_id}`}
                  className="flex items-center gap-3 border-b border-border/60 py-2.5 last:border-0"
                >
                  <span className="w-3 text-xs tabular-nums text-muted-foreground">
                    {index + 1}
                  </span>
                  <Avatar className="size-8 rounded-lg">
                    <AvatarFallback className="rounded-lg text-xs">
                      {(person.name || person.inviter_id).slice(0, 1)}
                    </AvatarFallback>
                  </Avatar>
                  <div className="min-w-0 flex-1">
                    <p className="truncate text-sm font-medium">
                      {person.name || person.inviter_id}
                    </p>
                    <p className="mt-0.5 truncate text-xs text-muted-foreground">
                      {roomTitle(person)}
                    </p>
                  </div>
                  <div className="text-right">
                    <strong className="text-lg font-semibold tabular-nums text-chart-join">
                      {person.joined}
                    </strong>
                    <p className="text-xs text-muted-foreground">
                      新增邀请
                    </p>
                  </div>
                </div>
              ))}
            </div>
          ) : (
            <EmptyState
              className="min-h-36 flex-1 gap-2 px-4 py-4"
              icon={UserRoundPlus}
              title="等待第一份邀请贡献"
              description="微信返回邀请来源后，会自动展示在这里。"
            />
          )}
          <p className="border-t border-border/60 px-4 py-2.5 text-xs text-muted-foreground">
            有邀请来源的扫码入群也会统计；未知来源不作推断。
          </p>
        </section>
      </div>
      <section className="mb-4 overflow-hidden rounded-lg border border-border/65 bg-card">
        <div className="flex items-center justify-between gap-2 px-4 py-3">
          <div>
            <h2 className="text-base font-semibold">最近成员动态</h2>
            <p className="mt-1 text-xs text-muted-foreground">
              最新 5 条记录 · 默认仅记录，不发送群消息
            </p>
          </div>
          <Button
            variant="ghost"
            size="sm"
            className="text-sm"
            onClick={() => navigate("history")}
          >
            查看全部
            <ArrowRight className="size-3.5" />
          </Button>
        </div>
        {loading ? (
          <LoadingState />
        ) : (
          <DataTable<Audit>
            rows={events}
            rowKey={(event) => String(event.id)}
            pageSize={0}
            columns={[
              {
                key: "member",
                header: "成员",
                cell: (event) => (
                  <IdentityCell
                    title={
                      event.name ||
                      (event.kind === "baseline"
                        ? "开始记录成员名单"
                        : event.member_id)
                    }
                    detail={event.member_id || undefined}
                    leading={
                      <Avatar className="size-7 rounded-md">
                        <AvatarFallback className="rounded-md text-xs">
                          {(event.name || "群").slice(0, 1)}
                        </AvatarFallback>
                      </Avatar>
                    }
                  />
                ),
              },
              {
                key: "room",
                header: "所在群聊",
                cell: (event) => (
                  <IdentityCell
                    title={roomTitle(event)}
                    detail={accountName(event.account_id)}
                  />
                ),
              },
              {
                key: "kind",
                header: "动态",
                cell: (event) => (
                  <div className="flex gap-1">
                    <Badge
                      variant={
                        event.kind === "joined"
                          ? "success"
                          : event.kind === "left"
                            ? "warning"
                            : "secondary"
                      }
                    >
                      {kinds[event.kind] || event.kind}
                    </Badge>
                    {event.gap && <Badge variant="outline">离线补记</Badge>}
                  </div>
                ),
              },
              {
                key: "time",
                header: "发现时间",
                className: "whitespace-nowrap text-sm text-muted-foreground",
                cell: (event) => formatTime(event.observed_at),
              },
            ]}
            empty={
              <EmptyState
                compact
                title="暂无成员动态"
                description="开启群记录后，成员的加入与离开将自动留档。"
              />
            }
          />
        )}
      </section>
      <section
        aria-label="新手引导"
        className="flex flex-col justify-between gap-3 rounded-lg bg-secondary/45 px-4 py-3 xl:flex-row xl:items-center"
      >
        <div>
          <h2 className="text-sm font-medium">让群管家开始工作</h2>
          <p className="mt-1 text-xs text-muted-foreground">
            首次只保存当前名单，之后有变化才记录。
          </p>
        </div>
        <div className="flex flex-wrap items-center gap-x-3 gap-y-2">
          {steps.map((step, index) => (
            <button
              key={step.text}
              onClick={() => navigate(index === 0 ? "settings" : "groups")}
              className="flex items-center gap-2 rounded text-xs hover:text-primary"
            >
              {step.done ? (
                <Check className="size-3.5 text-highlight" />
              ) : (
                <Circle className="size-3.5 text-muted-foreground" />
              )}
              <span
                className={
                  step.done ? "text-foreground" : "text-muted-foreground"
                }
              >
                {step.text}
              </span>
              {index !== 2 && (
                <ArrowRight className="ml-2 size-3 text-muted-foreground/50" />
              )}
            </button>
          ))}
        </div>
      </section>
    </>
  );
}
