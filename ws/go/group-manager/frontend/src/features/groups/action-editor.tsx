import { useEffect, useState } from "react";
import { ArrowRight } from "lucide-react";
import { api } from "@/lib/api";
import type { Action, GroupInfo, Room } from "@/lib/types";
import { actionNames } from "@/lib/format";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { ErrorState } from "@/components/shared/error-state";
import { LoadingState } from "@/components/shared/loading-state";
import type { ConfirmGroupAction } from "./group-members";

export function ActionEditor({
  room,
  action,
  onConfirm,
}: {
  room: Room;
  action: Action;
  onConfirm: ConfirmGroupAction;
}) {
  const [content, setContent] = useState(
    action === "name"
      ? room.name || ""
      : action === "remark"
        ? room.remark || ""
        : "",
  );
  const [enabled, setEnabled] = useState("true");
  const [loading, setLoading] = useState(action === "announcement");
  const [error, setError] = useState("");
  const [attempt, setAttempt] = useState(0);
  // 发布公告必须先读取当前内容；请求失败时不显示一个看似有效的空白表单。
  useEffect(() => {
    if (action !== "announcement") return;
    let active = true;
    setLoading(true);
    setError("");
    api<GroupInfo>("/api/group-info", {
      account_id: room.account_id,
      room_id: room.room_id,
    })
      .then((info) => {
        if (active) setContent(info.announcement || "");
      })
      .catch((reason: unknown) => {
        if (active)
          setError(reason instanceof Error ? reason.message : "读取公告失败");
      })
      .finally(() => {
        if (active) setLoading(false);
      });
    return () => {
      active = false;
    };
  }, [room.account_id, room.room_id, action, attempt]);
  if (loading)
    return (
      <LoadingState
        title="正在读取当前公告"
        description="读取成功后可在原内容上修改。"
      />
    );
  if (error && action === "announcement")
    return (
      <ErrorState
        message={error}
        onRetry={() => setAttempt((value) => value + 1)}
      />
    );
  const boolAction = action === "show_names" || action === "save_contact";
  const choiceText =
    enabled === "true"
      ? action === "save_contact"
        ? "保存到通讯录"
        : "显示成员群昵称"
      : action === "save_contact"
        ? "从通讯录移出"
        : "关闭成员群昵称显示";
  const submit = () => {
    if (action === "name" && !content.trim()) {
      setError("请填写群名称。");
      return;
    }
    onConfirm(
      action,
      boolAction ? { enabled: enabled === "true" } : { content },
      boolAction ? choiceText : content || "（清空）",
    );
  };
  return (
    <form
      className="space-y-4"
      noValidate
      onSubmit={(event) => {
        event.preventDefault();
        submit();
      }}
    >
      <div className="space-y-2">
        <label htmlFor="group-action-content" className="text-sm font-medium">
          {actionNames[action]}
        </label>
        {boolAction ? (
          <Select value={enabled} onValueChange={setEnabled}>
            <SelectTrigger id="group-action-content" className="w-full">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="true">
                {action === "save_contact" ? "保存到通讯录" : "显示成员群昵称"}
              </SelectItem>
              <SelectItem value="false">
                {action === "save_contact"
                  ? "从通讯录移出"
                  : "关闭成员群昵称显示"}
              </SelectItem>
            </SelectContent>
          </Select>
        ) : action === "announcement" ? (
          <Textarea
            id="group-action-content"
            value={content}
            onChange={(event) => setContent(event.target.value)}
            className="min-h-48"
            maxLength={12000}
          />
        ) : (
          <Input
            id="group-action-content"
            value={content}
            onChange={(event) => {
              setContent(event.target.value);
              setError("");
            }}
            maxLength={action === "remark" ? 12000 : 256}
            aria-invalid={!!error}
            aria-describedby={error ? "group-action-error" : undefined}
            autoFocus
          />
        )}
        <p className="text-xs leading-5 text-muted-foreground">
          {action === "remark"
            ? "仅当前微信账号可见。"
            : action === "self_name"
              ? "留空会清除我的群昵称。"
              : action === "announcement"
                ? "留空会提交清空公告，是否允许由微信判断。"
                : action === "name"
                  ? "群名称会对所有成员可见。"
                  : "此设置仅作用于当前微信账号。"}
        </p>
        {error && (
          <p
            id="group-action-error"
            role="alert"
            className="text-xs text-destructive"
          >
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
