import { useState } from "react";
import { Bell, RotateCcw, Save } from "lucide-react";
import { useAppData } from "@/app/data-context";
import { api } from "@/lib/api";
import type { Group, Room } from "@/lib/types";
import { groupKey } from "@/lib/format";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { Switch } from "@/components/ui/switch";
import { EmptyState } from "@/components/shared/empty-state";
import { ErrorState } from "@/components/shared/error-state";
import { LoadingState } from "@/components/shared/loading-state";

/** 草稿与轮询数据隔离。保存时只合并当前群，保留最近一次刷新得到的其他群设置。 */
export function GroupTracking({
  room,
  onReset,
  onSaved,
}: {
  room: Room;
  onReset: () => void;
  onSaved: () => void;
}) {
  const { config, persist, refresh, notify } = useAppData();
  const existing = config?.groups.find(
    (group) => groupKey(group) === groupKey(room),
  );
  const [draft, setDraft] = useState<Group>(
    () =>
      existing || {
        account_id: room.account_id,
        room_id: room.room_id,
        label: room.name || "",
        enabled: true,
        notify_join: false,
        notify_leave: false,
        join_template: "欢迎 {name} 加入本群。",
        leave_template: "成员 {name} 已离开本群。",
      },
  );
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  if (room.room_id.endsWith("@im.chatroom"))
    return (
      <EmptyState
        icon={Bell}
        title="暂不支持完整成员记录"
        description="企业互通群暂未提供完整成员明细，可继续使用群资料和昵称等设置。"
      />
    );
  if (!config) return <LoadingState title="正在读取记录设置" />;
  const update = <K extends keyof Group>(key: K, value: Group[K]) =>
    setDraft((current) => ({ ...current, [key]: value }));
  const save = async () => {
    if (busy) return;
    setBusy(true);
    setError("");
    try {
      const next = structuredClone(config);
      const index = next.groups.findIndex(
        (group) => groupKey(group) === groupKey(room),
      );
      const updated = { ...draft, label: draft.label.trim() };
      if (index < 0) next.groups.push(updated);
      else next.groups[index] = updated;
      await persist(next);
      onSaved();
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "保存群设置失败");
    } finally {
      setBusy(false);
    }
  };
  const recheck = async () => {
    if (busy) return;
    setBusy(true);
    setError("");
    try {
      await api("/api/recheck", {
        account_id: room.account_id,
        room_id: room.room_id,
        reset: false,
      });
      notify("已安排检查成员");
      await refresh();
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "安排检查失败");
    } finally {
      setBusy(false);
    }
  };
  return (
    <form
      className="space-y-4"
      onSubmit={(event) => {
        event.preventDefault();
        void save();
      }}
    >
      <div className="divide-y divide-border rounded-lg bg-muted/30 px-4">
        {(
          [
            [
              "enabled",
              "记录成员变化",
              "首次保存当前名单，之后记录新增与离群。",
            ],
            [
              "notify_join",
              "发送欢迎消息",
              "新成员加入时，自动向群里发送欢迎语。",
            ],
            [
              "notify_leave",
              "发送离群消息",
              "发现成员离开后，自动向群里发送提示。",
            ],
          ] as const
        ).map(([key, title, description]) => (
          <div
            key={key}
            className="flex items-center justify-between gap-4 py-3"
          >
            <div>
              <label
                htmlFor={`tracking-${key}`}
                className="cursor-pointer text-sm font-medium"
              >
                {title}
              </label>
              <p className="mt-1 text-xs leading-5 text-muted-foreground">
                {description}
              </p>
            </div>
            <Switch
              id={`tracking-${key}`}
              checked={draft[key]}
              onCheckedChange={(checked) => update(key, checked)}
              disabled={busy}
            />
          </div>
        ))}
      </div>
      <div className="space-y-2">
        <label htmlFor="tracking-label" className="text-sm font-medium">
          本地显示名称
        </label>
        <Input
          id="tracking-label"
          value={draft.label}
          onChange={(event) => update("label", event.target.value)}
          disabled={busy}
          maxLength={256}
        />
        <p className="text-xs text-muted-foreground">
          只用于管家显示，不修改微信群名称。
        </p>
      </div>
      <div className="grid gap-4 sm:grid-cols-2">
        <div className="space-y-2">
          <label htmlFor="tracking-welcome" className="text-sm font-medium">
            欢迎消息
          </label>
          <Textarea
            id="tracking-welcome"
            value={draft.join_template}
            onChange={(event) => update("join_template", event.target.value)}
            disabled={busy}
            maxLength={2000}
            className="min-h-28"
          />
        </div>
        <div className="space-y-2">
          <label htmlFor="tracking-leave" className="text-sm font-medium">
            离群消息
          </label>
          <Textarea
            id="tracking-leave"
            value={draft.leave_template}
            onChange={(event) => update("leave_template", event.target.value)}
            disabled={busy}
            maxLength={2000}
            className="min-h-28"
          />
        </div>
      </div>
      <p className="rounded-md bg-muted/40 px-3 py-2.5 text-xs leading-6 text-muted-foreground">
        {
          "可用变量：{name} 成员昵称、{wxid} 成员 ID、{group} 群名称、{count} 当前人数。关闭消息提醒仍会保留成员动态。"
        }
      </p>
      {error && <ErrorState message={error} />}
      <div className="flex flex-wrap items-center justify-between gap-3 border-t border-border pt-4">
        <div className="flex flex-wrap gap-2">
          {existing && (
            <>
              <Button
                variant="outline"
                size="sm"
                disabled={busy}
                onClick={() => void recheck()}
              >
                <RotateCcw />
                立即检查成员
              </Button>
              <Button
                variant="ghost"
                size="sm"
                disabled={busy}
                onClick={onReset}
              >
                重新开始记录
              </Button>
            </>
          )}
        </div>
        <Button type="submit" loading={busy}>
          <Save />
          保存群设置
        </Button>
      </div>
    </form>
  );
}
