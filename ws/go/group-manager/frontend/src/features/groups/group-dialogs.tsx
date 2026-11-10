import { useEffect, useState, type FormEvent } from "react";
import {
  ArrowLeft,
  ArrowRight,
  Bell,
  BookOpen,
  Bookmark,
  ChevronRight,
  FileText,
  LogOut,
  MessageSquare,
  Pencil,
  QrCode,
  Settings2,
  UserRound,
  Users,
} from "lucide-react";
import { useAppData } from "@/app/data-context";
import { api } from "@/lib/api";
import { actionNames, groupKey } from "@/lib/format";
import type { Action, Config, Group, Operation, Room } from "@/lib/types";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { ConfirmDialog } from "@/components/shared/confirm-dialog";
import { ErrorState } from "@/components/shared/error-state";
import { LoadingState } from "@/components/shared/loading-state";
import { ActionEditor } from "./action-editor";
import { GroupMembers, type ActionParams } from "./group-members";
import { GroupTracking } from "./group-tracking";
import { OperationResult } from "./operation-result";

export type GroupDialogTarget =
  | { type: "group"; room: Room }
  | { type: "manual" }
  | { type: "join" }
  | { type: "operation"; operation: Operation };
type Confirmation =
  | {
      type: "action";
      room: Room;
      action: Action;
      params: ActionParams;
      preview: string;
      id: string;
    }
  | { type: "reset"; room: Room };
type Stage = "group" | "edit" | "result" | "manual" | "join";

/**
 * 统一承载群管理、资料编辑、成员操作和结果页。
 * 每次新的操作确认生成一个稳定 UUID，超时后停留在同一个确认中，不生成新幂等键重发。
 * target 不承载宿主凭据，所有业务请求沿用现有本机 API。
 */
export function GroupDialogs({
  target,
  onClose,
}: {
  target: GroupDialogTarget | null;
  onClose: () => void;
}) {
  if (!target) return null;
  const identity =
    target.type === "group"
      ? `group:${groupKey(target.room)}`
      : target.type === "operation"
        ? `operation:${target.operation.id}`
        : target.type;
  return (
    <GroupDialogSession key={identity} target={target} onClose={onClose} />
  );
}
function GroupDialogSession({
  target,
  onClose,
}: {
  target: GroupDialogTarget;
  onClose: () => void;
}) {
  const {
    config,
    accountId,
    accountName,
    roomTitle,
    operations,
    refresh,
    notify,
  } = useAppData();
  const room = target.type === "group" ? target.room : null;
  const [stage, setStage] = useState<Stage>(
    target.type === "operation" ? "result" : target.type,
  );
  const [tab, setTab] = useState("manage");
  const [action, setAction] = useState<Action>("name");
  const [operation, setOperation] = useState<Operation | null>(
    target.type === "operation" ? target.operation : null,
  );
  const [confirmation, setConfirmation] = useState<Confirmation | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  // 当前操作仍由应用级轮询更新，打开详情不会另启一条写任务或自动重试。
  const operationId = operation?.id;
  useEffect(() => {
    if (!operationId) return;
    const latest = operations.find((item) => item.id === operationId);
    if (latest) setOperation(latest);
  }, [operations, operationId]);
  const confirmAction = (
    targetRoom: Room,
    nextAction: Action,
    params: ActionParams,
    preview: string,
  ) => {
    setConfirmation({
      type: "action",
      room: targetRoom,
      action: nextAction,
      params,
      preview,
      id: crypto.randomUUID(),
    });
  };
  const submit = async (
    targetRoom: Room,
    nextAction: Action,
    params: ActionParams,
    id: string,
  ) => {
    const response = await api<Operation>("/api/operation", {
      id,
      account_id: targetRoom.account_id,
      room_id: targetRoom.room_id,
      action: nextAction,
      ...params,
    });
    setOperation(response);
    setStage("result");
    setError("");
    notify("操作已提交，可在操作记录查看结果");
    await refresh();
  };
  const readAction = async (nextAction: Action) => {
    if (!room || busy) return;
    setBusy(true);
    setError("");
    try {
      await submit(room, nextAction, {}, crypto.randomUUID());
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "请求未完成");
    } finally {
      setBusy(false);
    }
  };
  const chooseAction = (nextAction: Action) => {
    if (!room) return;
    if (nextAction === "info" || nextAction === "qrcode") {
      void readAction(nextAction);
      return;
    }
    if (nextAction === "quit") {
      confirmAction(
        room,
        nextAction,
        {},
        "当前账号将退出这个群，并暂停成员记录。",
      );
      return;
    }
    setAction(nextAction);
    setStage("edit");
    setError("");
  };
  const confirm = async () => {
    if (!confirmation || busy) return;
    setBusy(true);
    try {
      if (confirmation.type === "reset") {
        await api("/api/recheck", {
          account_id: confirmation.room.account_id,
          room_id: confirmation.room.room_id,
          reset: true,
        });
        notify("已安排重新读取成员名单");
        await refresh();
      } else {
        await submit(
          confirmation.room,
          confirmation.action,
          confirmation.params,
          confirmation.id,
        );
      }
    } finally {
      setBusy(false);
    }
  };
  let title = "微信群管家";
  let description = "";
  if (stage === "group" && room) {
    title = roomTitle(room);
    description = `${room.room_id} · ${accountName(room.account_id)}`;
  } else if (stage === "edit" && room) {
    title = actionNames[action];
    description = `${roomTitle(room)} · ${accountName(room.account_id)}`;
  } else if (stage === "result" && operation) {
    title = actionNames[operation.action] || "操作详情";
    description = `${operation.room_id ? `${roomTitle(operation)} · ` : ""}${accountName(operation.account_id)}`;
  } else if (stage === "manual") {
    title = "手动添加群聊";
    description = "通常可直接同步群列表，手动添加适合已知群 ID 的情况。";
  } else if (stage === "join") {
    title = "通过链接入群";
    description = "使用当前微信账号，通过二维码或本人邀请链接加入群聊。";
  }
  const close = () => {
    if (!busy) onClose();
  };
  return (
    <>
      <Dialog
        open
        onOpenChange={(open) => {
          if (!open) close();
        }}
      >
        <DialogContent
          className="max-h-[90dvh] overflow-y-auto sm:max-w-3xl"
          onEscapeKeyDown={(event) => {
            if (busy || confirmation) event.preventDefault();
          }}
          onPointerDownOutside={(event) => {
            if (busy || confirmation) event.preventDefault();
          }}
        >
          <DialogHeader>
            <DialogTitle className="pr-6">{title}</DialogTitle>
            <DialogDescription className="break-all">
              {description}
            </DialogDescription>
          </DialogHeader>
          {stage === "edit" && room && (
            <div>
              <Button
                variant="ghost"
                size="sm"
                className="mb-4 -ml-2"
                onClick={() => {
                  setStage("group");
                  setError("");
                }}
              >
                <ArrowLeft />
                返回群管理
              </Button>
              <ActionEditor
                key={action}
                room={room}
                action={action}
                onConfirm={(nextAction, params, preview) =>
                  confirmAction(room, nextAction, params, preview)
                }
              />
            </div>
          )}
          {stage === "group" && room && (
            <Tabs value={tab} onValueChange={setTab}>
              <TabsList className="mb-4 grid w-full grid-cols-3">
                <TabsTrigger value="manage" className="gap-2">
                  <Settings2 className="hidden size-4 sm:block" />
                  资料与设置
                </TabsTrigger>
                <TabsTrigger value="members" className="gap-2">
                  <Users className="hidden size-4 sm:block" />
                  群成员
                </TabsTrigger>
                <TabsTrigger value="tracking" className="gap-2">
                  <Bell className="hidden size-4 sm:block" />
                  记录与提醒
                </TabsTrigger>
              </TabsList>
              <TabsContent value="manage">
                <GroupActions room={room} busy={busy} onAction={chooseAction} />
              </TabsContent>
              <TabsContent value="members">
                <GroupMembers
                  room={room}
                  onAction={(nextAction, params, preview) =>
                    confirmAction(room, nextAction, params, preview)
                  }
                />
              </TabsContent>
              <TabsContent value="tracking">
                {config ? (
                  <GroupTracking
                    room={room}
                    onReset={() => setConfirmation({ type: "reset", room })}
                    onSaved={onClose}
                  />
                ) : (
                  <LoadingState title="正在读取群设置" />
                )}
              </TabsContent>
            </Tabs>
          )}
          {stage === "result" && operation && (
            <OperationResult operation={operation} onUpdate={setOperation} />
          )}
          {stage === "manual" &&
            (!config ? (
              <LoadingState title="正在读取群聊设置" />
            ) : !accountId ? (
              <ErrorState message="请先关闭窗口，在顶部选择一个微信账号。" />
            ) : (
              <ManualGroup
                accountId={accountId}
                config={config}
                onSaved={onClose}
              />
            ))}
          {stage === "join" &&
            (!accountId ? (
              <ErrorState message="请先关闭窗口，在顶部选择一个微信账号。" />
            ) : (
              <JoinByLink
                onConfirm={(nextAction, params, preview) =>
                  confirmAction(
                    { account_id: accountId, room_id: "" },
                    nextAction,
                    params,
                    preview,
                  )
                }
              />
            ))}
          {error && <ErrorState message={error} />}
        </DialogContent>
      </Dialog>
      {confirmation && (
        <ConfirmDialog
          open
          onOpenChange={(open) => {
            if (!open) setConfirmation(null);
          }}
          title={
            confirmation.type === "reset"
              ? "重新开始记录"
              : `确认${actionNames[confirmation.action]}`
          }
          description={`${confirmation.room.room_id ? `${roomTitle(confirmation.room)} · ` : ""}${accountName(confirmation.room.account_id)}`}
          onConfirm={confirm}
          loading={busy}
          destructive={
            confirmation.type === "action" &&
            ["quit", "remove_members", "transfer_owner"].includes(
              confirmation.action,
            )
          }
          confirmLabel={
            confirmation.type === "reset" ? "确认重新开始" : "确认提交"
          }
        >
          <div className="max-h-64 overflow-y-auto whitespace-pre-wrap break-words rounded-md bg-muted/50 p-4 text-sm leading-6">
            {confirmation.type === "reset"
              ? "将以微信当前成员名单作为新的记录起点，保留以前的日志。本次加载不会把所有成员记成新加入。"
              : confirmation.preview}
          </div>
          {confirmation.type === "action" &&
            ["quit", "remove_members", "transfer_owner"].includes(
              confirmation.action,
            ) && (
              <p className="text-xs leading-5 text-muted-foreground">
                请核对群聊和成员，提交后由微信按当前账号权限执行。
              </p>
            )}
        </ConfirmDialog>
      )}
    </>
  );
}
const actions = [
  {
    id: "info",
    title: "查看群公告",
    description: "当前公告及发布信息",
    icon: FileText,
  },
  {
    id: "announcement",
    title: "发布群公告",
    description: "编辑或清空群公告",
    icon: Bell,
  },
  {
    id: "name",
    title: "修改群名称",
    description: "所有群成员可见的名称",
    icon: Pencil,
  },
  {
    id: "remark",
    title: "设置群备注",
    description: "仅当前微信账号可见",
    icon: Bookmark,
  },
  {
    id: "self_name",
    title: "我的群昵称",
    description: "设置或清除本群内昵称",
    icon: UserRound,
  },
  {
    id: "show_names",
    title: "成员昵称显示",
    description: "显示或隐藏成员群昵称",
    icon: MessageSquare,
  },
  {
    id: "save_contact",
    title: "保存到通讯录",
    description: "保存本群或从通讯录移出",
    icon: BookOpen,
  },
  {
    id: "qrcode",
    title: "群二维码",
    description: "获取微信返回的群二维码",
    icon: QrCode,
  },
  {
    id: "quit",
    title: "退出群聊",
    description: "退出后暂停本群成员记录",
    icon: LogOut,
  },
] as const;
function GroupActions({
  room,
  busy,
  onAction,
}: {
  room: Room;
  busy: boolean;
  onAction: (action: Action) => void;
}) {
  const enterprise = room.room_id.endsWith("@im.chatroom");
  return (
    <div className="space-y-4">
      <div className="grid gap-2 sm:grid-cols-2">
        {actions
          .filter(
            (item) => !enterprise || !["remark", "qrcode"].includes(item.id),
          )
          .map(({ id, title, description, icon: Icon }) => (
            <button
              key={id}
              type="button"
              disabled={busy}
              onClick={() => onAction(id)}
              className={`group flex min-w-0 items-center gap-3 rounded-lg border border-transparent px-3 py-2.5 text-left transition-colors hover:border-border hover:bg-accent focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:pointer-events-none disabled:opacity-45 ${id === "quit" ? "text-destructive" : "text-foreground"}`}
            >
              <span
                className={`flex size-9 shrink-0 items-center justify-center rounded-md ${id === "quit" ? "bg-destructive/10" : "bg-highlight/10 text-highlight"}`}
              >
                <Icon className="size-4" />
              </span>
              <span className="min-w-0 flex-1">
                <span className="block text-sm font-medium">{title}</span>
                <span className="mt-0.5 block text-xs text-muted-foreground">
                  {description}
                </span>
              </span>
              <ChevronRight className="size-3.5 shrink-0 text-muted-foreground opacity-50 transition-opacity group-hover:opacity-100" />
            </button>
          ))}
      </div>
      <p className="border-t border-border pt-3 text-xs leading-5 text-muted-foreground">
        管理员设置、群主转让和移出成员请进入「群成员」。实际权限由微信校验。
      </p>
    </div>
  );
}
function ManualGroup({
  accountId,
  config,
  onSaved,
}: {
  accountId: string;
  config: Config;
  onSaved: () => void;
}) {
  const { persist } = useAppData();
  const [id, setId] = useState("");
  const [label, setLabel] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const save = async (event: FormEvent) => {
    event.preventDefault();
    if (busy) return;
    const roomId = id.trim();
    if (
      !roomId.endsWith("@chatroom") ||
      roomId.length <= "@chatroom".length ||
      /[\s,]/.test(roomId) ||
      roomId.includes(String.fromCharCode(0))
    ) {
      setError("请填写有效的普通微信群 ID，例如 123456@chatroom。");
      return;
    }
    if (
      config.groups.some(
        (group) => group.account_id === accountId && group.room_id === roomId,
      )
    ) {
      setError("这个群已添加，请在群聊管理中开启记录或修改设置。");
      return;
    }
    setBusy(true);
    setError("");
    try {
      const group: Group = {
        account_id: accountId,
        room_id: roomId,
        label: label.trim(),
        enabled: true,
        notify_join: false,
        notify_leave: false,
        join_template: "欢迎 {name} 加入本群。",
        leave_template: "成员 {name} 已离开本群。",
      };
      await persist({ ...config, groups: [...config.groups, group] });
      onSaved();
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "添加群聊失败");
    } finally {
      setBusy(false);
    }
  };
  return (
    <form
      className="space-y-4"
      noValidate
      onSubmit={(event) => void save(event)}
    >
      <div className="space-y-2">
        <label htmlFor="manual-group-id" className="text-sm font-medium">
          群 ID
        </label>
        <Input
          id="manual-group-id"
          value={id}
          onChange={(event) => {
            setId(event.target.value);
            setError("");
          }}
          placeholder="123456@chatroom"
          disabled={busy}
          autoFocus
          aria-invalid={!!error}
        />
      </div>
      <div className="space-y-2">
        <label htmlFor="manual-group-label" className="text-sm font-medium">
          本地显示名称
        </label>
        <Input
          id="manual-group-label"
          value={label}
          onChange={(event) => setLabel(event.target.value)}
          placeholder="例如：客户交流群"
          disabled={busy}
          maxLength={256}
        />
        <p className="text-xs text-muted-foreground">
          只用于管家显示，后续可以修改。
        </p>
      </div>
      <p className="rounded-md bg-muted/40 p-3 text-xs leading-5 text-muted-foreground">
        首次保存当前名单，后续发现的成员变化会记录日志。欢迎消息和离群消息默认关闭。
      </p>
      {error && (
        <p role="alert" className="text-sm text-destructive">
          {error}
        </p>
      )}
      <div className="flex justify-end border-t border-border pt-4">
        <Button type="submit" loading={busy}>
          添加并开始记录
        </Button>
      </div>
    </form>
  );
}
function JoinByLink({
  onConfirm,
}: {
  onConfirm: (action: Action, params: ActionParams, preview: string) => void;
}) {
  const [kind, setKind] = useState<Action>("join_scan");
  const [url, setUrl] = useState("");
  const [error, setError] = useState("");
  const choices = [
    { id: "join_scan", label: "普通群二维码链接" },
    { id: "join_scan_enterprise", label: "企业互通群二维码链接" },
    { id: "accept_invite", label: "确认本人收到的邀请链接" },
  ] as const;
  const submit = (event: FormEvent) => {
    event.preventDefault();
    let parsed: URL;
    try {
      parsed = new URL(url.trim());
    } catch {
      setError("请粘贴完整的微信原始链接。");
      return;
    }
    if (!["https:", "http:"].includes(parsed.protocol)) {
      setError("链接必须以 https:// 或 http:// 开头。");
      return;
    }
    onConfirm(
      kind,
      { url: url.trim() },
      `${choices.find((choice) => choice.id === kind)?.label}\n${url.trim()}`,
    );
  };
  return (
    <form className="space-y-4" noValidate onSubmit={submit}>
      <div className="space-y-2">
        <label htmlFor="join-link-kind" className="text-sm font-medium">
          链接类型
        </label>
        <Select
          value={kind}
          onValueChange={(value) => setKind(value as Action)}
        >
          <SelectTrigger id="join-link-kind" className="w-full">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {choices.map((choice) => (
              <SelectItem key={choice.id} value={choice.id}>
                {choice.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>
      <div className="space-y-2">
        <label htmlFor="join-link-url" className="text-sm font-medium">
          微信原始链接
        </label>
        <Input
          id="join-link-url"
          value={url}
          onChange={(event) => {
            setUrl(event.target.value);
            setError("");
          }}
          placeholder="https://weixin.qq.com/..."
          aria-invalid={!!error}
        />
        <p className="text-xs leading-5 text-muted-foreground">
          请粘贴从二维码或本人邀请消息取得的完整链接。
        </p>
        {error && (
          <p role="alert" className="text-xs text-destructive">
            {error}
          </p>
        )}
      </div>
      <div className="flex justify-end border-t border-border pt-4">
        <Button type="submit">
          核对并继续
          <ArrowRight />
        </Button>
      </div>
    </form>
  );
}
