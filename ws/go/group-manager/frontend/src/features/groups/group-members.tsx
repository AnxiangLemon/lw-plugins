import { useEffect, useMemo, useState } from "react";
import { ShieldCheck, UserMinus, UserRoundCog } from "lucide-react";
import { api } from "@/lib/api";
import type {
  Action,
  Member,
  OperationInput,
  Room,
  Snapshot,
} from "@/lib/types";
import { actionNames } from "@/lib/format";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Checkbox } from "@/components/ui/checkbox";
import { DataTable } from "@/components/shared/data-table";
import { SearchInput } from "@/components/shared/search-input";
import { EmptyState } from "@/components/shared/empty-state";
import { ErrorState } from "@/components/shared/error-state";
import { LoadingState } from "@/components/shared/loading-state";

export type ActionParams = Pick<
  OperationInput,
  "content" | "members" | "enabled" | "url"
>;
export type ConfirmGroupAction = (
  action: Action,
  params: ActionParams,
  preview: string,
) => void;
const memberName = (member: Member) =>
  member.displayName || member.nickName || member.userName;

/** 名单来自实时完整成员接口；界面选择不会触发微信写操作，最终操作统一进入确认流程。 */
export function GroupMembers({
  room,
  onAction,
}: {
  room: Room;
  onAction: ConfirmGroupAction;
}) {
  const [members, setMembers] = useState<Member[] | null>(null);
  const [error, setError] = useState("");
  const [query, setQuery] = useState("");
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [attempt, setAttempt] = useState(0);
  const enterprise = room.room_id.endsWith("@im.chatroom");
  useEffect(() => {
    if (enterprise) return;
    let active = true;
    setMembers(null);
    setError("");
    setSelected(new Set());
    api<Snapshot>("/api/group-members", {
      account_id: room.account_id,
      room_id: room.room_id,
    })
      .then((snapshot) => {
        if (active) setMembers(snapshot.members || []);
      })
      .catch((reason: unknown) => {
        if (active)
          setError(reason instanceof Error ? reason.message : "读取成员失败");
      });
    return () => {
      active = false;
    };
  }, [room.account_id, room.room_id, enterprise, attempt]);
  const names = useMemo(
    () =>
      new Map(
        (members || []).map((member) => [member.userName, memberName(member)]),
      ),
    [members],
  );
  const rows = useMemo(
    () =>
      (members || []).filter((member) =>
        [member.userName, member.nickName, member.displayName].some((value) =>
          value?.toLocaleLowerCase().includes(query.toLocaleLowerCase()),
        ),
      ),
    [members, query],
  );
  if (enterprise)
    return (
      <EmptyState
        title="暂不支持完整成员管理"
        description="企业互通群暂未提供完整成员明细，可继续使用群资料和昵称等设置。"
      />
    );
  if (error)
    return (
      <ErrorState
        message={error}
        onRetry={() => setAttempt((value) => value + 1)}
      />
    );
  if (!members)
    return (
      <LoadingState
        title="正在读取群成员"
        description="从微信获取完整名单，请稍候。"
      />
    );
  const confirmMembers = (action: Action) => {
    const ids = [...selected];
    onAction(
      action,
      { members: ids },
      ids.map((id) => `${names.get(id) || id} · ${id}`).join("\n"),
    );
  };
  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <SearchInput
          value={query}
          onChange={setQuery}
          placeholder="搜索成员昵称或 ID"
          aria-label="搜索群成员"
          className="w-full sm:max-w-xs"
        />
        <span className="text-xs text-muted-foreground">
          共 {members.length} 位成员 · 已选择 {selected.size} 位
        </span>
      </div>
      <DataTable<Member>
        rows={rows}
        rowKey={(member) => member.userName}
        pageSize={10}
        pageKey={query}
        empty={
          <EmptyState
            compact
            title="没有匹配的成员"
            description="试试其他昵称或微信 ID。"
          />
        }
        columns={[
          {
            key: "select",
            header: "选择",
            className: "w-12",
            cell: (member) => (
              <Checkbox
                aria-label={`选择 ${memberName(member)}`}
                disabled={member.userName === room.account_id}
                checked={selected.has(member.userName)}
                onCheckedChange={(checked) =>
                  setSelected((previous) => {
                    const next = new Set(previous);
                    if (checked === true) next.add(member.userName);
                    else next.delete(member.userName);
                    return next;
                  })
                }
              />
            ),
          },
          {
            key: "member",
            header: "群成员",
            cell: (member) => (
              <div className="min-w-36">
                <div className="flex items-center gap-2">
                  <span className="font-medium">{memberName(member)}</span>
                  {member.userName === room.owner && <Badge>群主</Badge>}
                  {member.userName === room.account_id && (
                    <Badge variant="outline">我</Badge>
                  )}
                </div>
                <div className="mt-1 text-xs text-muted-foreground">
                  {member.userName}
                </div>
              </div>
            ),
          },
          {
            key: "nickname",
            header: "微信昵称",
            cell: (member) => member.nickName || "—",
          },
          {
            key: "inviter",
            header: "邀请来源",
            cell: (member) =>
              member.inviterUserName ? (
                <div>
                  <div>
                    {names.get(member.inviterUserName) ||
                      member.inviterUserName}
                  </div>
                  <div className="mt-1 text-xs text-muted-foreground">
                    {member.inviterUserName}
                  </div>
                </div>
              ) : (
                <span className="text-muted-foreground">来源未知</span>
              ),
          },
        ]}
      />
      <div className="flex flex-wrap gap-2 border-t border-border pt-4">
        <Button
          variant="outline"
          size="sm"
          disabled={!selected.size}
          onClick={() => confirmMembers("add_admin")}
        >
          <ShieldCheck />
          {actionNames.add_admin}
        </Button>
        <Button
          variant="outline"
          size="sm"
          disabled={!selected.size}
          onClick={() => confirmMembers("remove_admin")}
        >
          <ShieldCheck />
          {actionNames.remove_admin}
        </Button>
        <Button
          variant="outline"
          size="sm"
          disabled={selected.size !== 1}
          onClick={() => confirmMembers("transfer_owner")}
        >
          <UserRoundCog />
          {actionNames.transfer_owner}
        </Button>
        <Button
          variant="destructive"
          size="sm"
          disabled={!selected.size}
          onClick={() => confirmMembers("remove_members")}
        >
          <UserMinus />
          {actionNames.remove_members}
        </Button>
      </div>
      <p className="text-xs leading-5 text-muted-foreground">
        转让群主请只选择一位成员。微信会校验当前账号的群主或管理员权限。
      </p>
    </div>
  );
}
