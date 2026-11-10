import { useMemo, useState } from "react";
import {
  Bell,
  FileText,
  Link,
  MessageSquare,
  Pause,
  Play,
  Plus,
  RefreshCw,
  Settings2,
} from "lucide-react";
import { useAppData } from "@/app/data-context";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Checkbox } from "@/components/ui/checkbox";
import { Avatar, AvatarFallback } from "@/components/ui/avatar";
import { DataTable } from "@/components/shared/data-table";
import { EmptyState } from "@/components/shared/empty-state";
import { LoadingState } from "@/components/shared/loading-state";
import { ErrorState } from "@/components/shared/error-state";
import { PageHeader } from "@/components/shared/page-header";
import { SearchInput } from "@/components/shared/search-input";
import { GroupDialogs } from "@/features/groups/group-dialogs";
import type { ComponentProps } from "react";
import type { Room } from "@/lib/types";
import { api } from "@/lib/api";
import { formatTime, groupKey } from "@/lib/format";
import { IdentityCell } from "./page-controls";

export function GroupsPage() {
  const {
    status,
    config,
    rooms,
    accountId,
    loading,
    error,
    refresh,
    persist,
    notify,
    roomTitle,
    accountName,
  } = useAppData();
  const [query, setQuery] = useState("");
  const [trackedOnly, setTrackedOnly] = useState(false);
  const [pending, setPending] = useState("");
  const [target, setTarget] =
    useState<ComponentProps<typeof GroupDialogs>["target"]>(null);
  const visibleRooms = rooms.filter(
    (room) => !accountId || room.account_id === accountId,
  );
  const states = useMemo(
    () =>
      new Map((status?.groups || []).map((group) => [groupKey(group), group])),
    [status?.groups],
  );
  const filtered = visibleRooms.filter((room) => {
    const search = query.trim().toLocaleLowerCase();
    return (
      (!search ||
        [room.name, room.remark, room.room_id, room.label].some((value) =>
          value?.toLocaleLowerCase().includes(search),
        )) &&
      (!trackedOnly || states.get(groupKey(room))?.enabled)
    );
  });
  const directories = (status?.directory || []).filter(
    (item) => !accountId || item.account_id === accountId,
  );
  const syncing = directories.some((item) => item.running);
  const failure = directories.find((item) => item.error);
  const latest = directories
    .filter(
      (item) =>
        !item.error && item.updated_at && !item.updated_at.startsWith("0001"),
    )
    .sort((a, b) => b.updated_at.localeCompare(a.updated_at))[0];

  async function syncRooms() {
    if (!accountId || pending) return;
    setPending("sync");
    try {
      await api("/api/discover", { account_id: accountId });
      notify("已开始同步微信群聊，群名称和备注会自动更新");
      await refresh();
    } catch (cause) {
      notify(cause instanceof Error ? cause.message : "同步群聊失败", "error");
    } finally {
      setPending("");
    }
  }

  async function toggleTracking(room: Room) {
    if (!config || pending || room.room_id.endsWith("@im.chatroom")) return;
    setPending(groupKey(room));
    // 配置更新沿用原接口。新群始终只记录日志，不自动开启任何发消息开关。
    const next = structuredClone(config);
    const existing = next.groups.find(
      (group) => groupKey(group) === groupKey(room),
    );
    if (existing) existing.enabled = !existing.enabled;
    else
      next.groups.push({
        account_id: room.account_id,
        room_id: room.room_id,
        label: room.name || room.label || "",
        enabled: true,
        notify_join: false,
        notify_leave: false,
        join_template: "欢迎 {name} 加入本群。",
        leave_template: "成员 {name} 已离开本群。",
      });
    try {
      await persist(next);
    } catch (cause) {
      notify(
        cause instanceof Error ? cause.message : "保存群设置失败",
        "error",
      );
    } finally {
      setPending("");
    }
  }

  return (
    <div className="space-y-4">
      <PageHeader
        title="群聊管理"
        description="管理群资料与成员，为需要关注的群开启记录。"
        actions={
          <>
            <Button
              variant="outline"
              onClick={() => setTarget({ type: "join" })}
              disabled={!accountId}
            >
              <Link className="size-4" />
              通过链接入群
            </Button>
            <Button
              variant="outline"
              onClick={() => setTarget({ type: "manual" })}
              disabled={!accountId}
            >
              <Plus className="size-4" />
              手动添加
            </Button>
            <Button
              onClick={() => void syncRooms()}
              loading={pending === "sync" || syncing}
              disabled={!accountId || !status?.connected || Boolean(pending)}
            >
              <RefreshCw className="size-4" />
              同步微信群聊
            </Button>
          </>
        }
      />
      {error && !status ? (
        <ErrorState message={error} onRetry={() => void refresh()} />
      ) : loading && !status ? (
        <LoadingState />
      ) : (
        <>
          <div className="flex flex-wrap items-center justify-between gap-3">
            <div className="w-full sm:max-w-sm">
              <SearchInput
                value={query}
                onChange={setQuery}
                placeholder="搜索群名称、备注或群 ID"
              />
            </div>
            <label className="flex cursor-pointer items-center gap-2 text-sm text-muted-foreground">
              <Checkbox
                checked={trackedOnly}
                onCheckedChange={(checked) => setTrackedOnly(checked === true)}
              />
              只看已开启记录
            </label>
          </div>
          <div
            className="flex flex-wrap items-center gap-x-4 gap-y-1 text-xs text-muted-foreground"
            aria-live="polite"
          >
            <span>
              共{" "}
              <strong className="font-medium text-foreground">
                {visibleRooms.length}
              </strong>{" "}
              个群聊 ·{" "}
              <strong className="font-medium text-foreground">
                {
                  visibleRooms.filter(
                    (room) => states.get(groupKey(room))?.enabled,
                  ).length
                }
              </strong>{" "}
              个正在记录
            </span>
            <span className={failure ? "text-warning" : ""}>
              {syncing
                ? "正在从微信同步群聊和群名称…"
                : failure
                  ? `群列表同步未完成：${failure.error}`
                  : latest
                    ? `最近同步 ${formatTime(latest.updated_at)}`
                    : accountId
                      ? "连接微信后会自动获取群列表，也可以手动同步。"
                      : "先在右上角选择微信账号，即可同步或添加群聊。"}
            </span>
          </div>
          <div className="overflow-hidden rounded-lg border border-border bg-card">
            <DataTable
              rows={filtered}
              rowKey={groupKey}
              pageKey={`${accountId}|${query}|${trackedOnly}`}
              empty={
                <EmptyState
                  compact
                  title={
                    query || trackedOnly
                      ? "没有符合条件的群聊"
                      : "开始管理你的第一个群"
                  }
                  description={
                    query || trackedOnly
                      ? "试试其他关键词，或关闭“只看已开启记录”。"
                      : "选择已登录的微信账号，同步群列表后为需要的群开启记录。"
                  }
                  action={
                    <Button
                      variant="outline"
                      disabled={!accountId || !status?.connected || syncing}
                      onClick={() => void syncRooms()}
                    >
                      <RefreshCw className="size-4" />
                      同步群聊
                    </Button>
                  }
                />
              }
              columns={[
                {
                  key: "room",
                  header: "群聊",
                  cell: (room: Room) => (
                    <IdentityCell
                      title={roomTitle(room)}
                      detail={`${room.room_id} · ${accountName(room.account_id)}`}
                      leading={
                        <Avatar className="size-9 rounded-lg">
                          <AvatarFallback className="rounded-lg bg-highlight/10 text-highlight">
                            <MessageSquare className="size-4" />
                          </AvatarFallback>
                        </Avatar>
                      }
                    />
                  ),
                },
                {
                  key: "members",
                  header: "成员",
                  cell: (room: Room) => (
                    <span className="tabular-nums">
                      {states.get(groupKey(room))?.member_count || "—"}
                    </span>
                  ),
                },
                {
                  key: "tracking",
                  header: "记录状态",
                  cell: (room: Room) => {
                    const state = states.get(groupKey(room));
                    return (
                      <Badge
                        variant={
                          state?.error
                            ? "warning"
                            : state?.enabled
                              ? "success"
                              : "secondary"
                        }
                        title={state?.error}
                      >
                        {state?.error
                          ? "检查遇到问题"
                          : state?.enabled
                            ? state.running
                              ? "正在同步"
                              : state.pending
                                ? "等待检查"
                                : "正在记录"
                            : room.room_id.endsWith("@im.chatroom")
                              ? "仅资料管理"
                              : "未开启"}
                      </Badge>
                    );
                  },
                },
                {
                  key: "notifications",
                  header: "消息提醒",
                  cell: (room: Room) => {
                    const state = states.get(groupKey(room));
                    const enabled =
                      state?.enabled &&
                      (state.notify_join || state.notify_leave);
                    return (
                      <span
                        className={`inline-flex items-center gap-1.5 text-sm ${enabled ? "text-success" : "text-muted-foreground"}`}
                      >
                        {enabled ? (
                          <Bell className="size-3.5" />
                        ) : (
                          <FileText className="size-3.5" />
                        )}
                        {enabled ? "已开启提醒" : "仅记录"}
                      </span>
                    );
                  },
                },
                {
                  key: "checked",
                  header: "最近检查",
                  className: "whitespace-nowrap text-xs text-muted-foreground",
                  cell: (room: Room) =>
                    formatTime(states.get(groupKey(room))?.last_checked),
                },
                {
                  key: "actions",
                  header: "操作",
                  cell: (room: Room) => (
                    <div className="flex items-center gap-1">
                      <Button
                        variant="ghost"
                        size="sm"
                        onClick={() => setTarget({ type: "group", room })}
                      >
                        <Settings2 className="size-3.5" />
                        管理
                      </Button>
                      <Button
                        variant="outline"
                        size="sm"
                        loading={pending === groupKey(room)}
                        disabled={
                          Boolean(pending) ||
                          room.room_id.endsWith("@im.chatroom")
                        }
                        onClick={() => void toggleTracking(room)}
                      >
                        {states.get(groupKey(room))?.enabled ? (
                          <Pause className="size-3.5" />
                        ) : (
                          <Play className="size-3.5" />
                        )}
                        {states.get(groupKey(room))?.enabled
                          ? "暂停记录"
                          : "开启记录"}
                      </Button>
                    </div>
                  ),
                },
              ]}
            />
          </div>
        </>
      )}
      <GroupDialogs target={target} onClose={() => setTarget(null)} />
    </div>
  );
}
