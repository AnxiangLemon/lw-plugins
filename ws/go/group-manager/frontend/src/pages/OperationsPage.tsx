import { useState, type ComponentProps } from "react";
import { Eye, Info, RefreshCw } from "lucide-react";
import { useAppData } from "@/app/data-context";
import { api } from "@/lib/api";
import { actionNames, formatTime, statuses } from "@/lib/format";
import type { Operation } from "@/lib/types";
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
import { LoadingState } from "@/components/shared/loading-state";
import { ErrorState } from "@/components/shared/error-state";
import { GroupDialogs } from "@/features/groups/group-dialogs";
import { IdentityCell } from "./page-controls";

export function OperationsPage() {
  const {
    operations,
    accountId,
    status,
    loading,
    error,
    refresh,
    notify,
    roomTitle,
    accountName,
  } = useAppData();
  const [query, setQuery] = useState("");
  const [filter, setFilter] = useState("all");
  const [pending, setPending] = useState("");
  const [target, setTarget] =
    useState<ComponentProps<typeof GroupDialogs>["target"]>(null);
  const rows = operations.filter(
    (operation) =>
      (!accountId || operation.account_id === accountId) &&
      (filter === "all" ||
        (filter === "processing"
          ? ["queued", "running"].includes(operation.status)
          : filter === "failed"
            ? ["failed", "rejected", "partial", "cancelled"].includes(
                operation.status,
              )
            : operation.status === filter)) &&
      (!query.trim() ||
        [
          actionNames[operation.action] || operation.action,
          operation.room_id ? roomTitle(operation) : "通过链接入群",
          accountName(operation.account_id),
          operation.id,
        ].some((value) =>
          value.toLocaleLowerCase().includes(query.trim().toLocaleLowerCase()),
        )),
  );

  async function queryReceipt(operation: Operation) {
    if (pending) return;
    setPending(operation.id);
    try {
      const result = await api<Operation>("/api/operation-receipt", {
        id: operation.id,
      });
      setTarget({ type: "operation", operation: result });
      await refresh();
    } catch (cause) {
      notify(
        cause instanceof Error ? cause.message : "查询原操作结果失败",
        "error",
      );
    } finally {
      setPending("");
    }
  }

  return (
    <div className="space-y-4">
      <PageHeader
        title="操作记录"
        description="公告、群设置与成员管理，每一次操作都有记录。"
        actions={
          <Button
            variant="outline"
            loading={loading}
            onClick={() => void refresh()}
          >
            <RefreshCw className="size-4" />
            刷新数据
          </Button>
        }
      />
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="w-full sm:max-w-sm">
          <SearchInput
            value={query}
            onChange={setQuery}
            placeholder="搜索操作、群聊或操作编号"
          />
        </div>
        <Select value={filter} onValueChange={setFilter}>
          <SelectTrigger className="w-full sm:w-44" aria-label="按操作状态筛选">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">全部状态</SelectItem>
            <SelectItem value="succeeded">已完成</SelectItem>
            <SelectItem value="processing">处理中</SelectItem>
            <SelectItem value="unknown">结果待确认</SelectItem>
            <SelectItem value="failed">未完成 / 已取消</SelectItem>
          </SelectContent>
        </Select>
      </div>
      <div className="flex items-start gap-2.5 rounded-md bg-secondary/60 px-4 py-3 text-xs leading-6 text-muted-foreground">
        <Info className="mt-1 size-3.5 shrink-0 text-highlight" />
        <span>
          显示“结果待确认”时，查询原操作即可。群主、管理员等操作权限由微信校验，操作结果会保存在本机。
        </span>
      </div>
      {error && !status ? (
        <ErrorState message={error} onRetry={() => void refresh()} />
      ) : loading && !status ? (
        <LoadingState />
      ) : (
        <div className="overflow-hidden rounded-lg border border-border bg-card">
          <div className="flex items-center justify-between border-b border-border px-4 py-3">
            <h2 className="text-base font-semibold">最近操作</h2>
            <span className="text-xs text-muted-foreground">
              最近 100 条 · {rows.length} 条匹配
            </span>
          </div>
          <DataTable
            rows={rows}
            rowKey={(operation: Operation) => operation.id}
            pageKey={`${accountId}|${query}|${filter}`}
            empty={
              <EmptyState
                compact
                title={
                  query || filter !== "all"
                    ? "没有符合条件的操作"
                    : "还没有操作记录"
                }
                description={
                  query || filter !== "all"
                    ? "试试其他关键词或操作状态。"
                    : "在群聊管理中修改公告、群设置或管理成员，处理结果会显示在这里。"
                }
              />
            }
            columns={[
              {
                key: "action",
                header: "操作",
                cell: (operation: Operation) => (
                  <IdentityCell
                    title={actionNames[operation.action] || operation.action}
                    detail={operation.id}
                  />
                ),
              },
              {
                key: "room",
                header: "群聊 / 账号",
                cell: (operation: Operation) => (
                  <IdentityCell
                    title={
                      operation.room_id ? roomTitle(operation) : "通过链接入群"
                    }
                    detail={accountName(operation.account_id)}
                  />
                ),
              },
              {
                key: "status",
                header: "状态",
                cell: (operation: Operation) => (
                  <Badge
                    variant={
                      operation.status === "succeeded"
                        ? "success"
                        : ["failed", "rejected", "partial"].includes(
                              operation.status,
                            )
                          ? "destructive"
                          : operation.status === "unknown"
                            ? "warning"
                            : "secondary"
                    }
                  >
                    {statuses[operation.status] || operation.status}
                  </Badge>
                ),
              },
              {
                key: "time",
                header: "提交时间",
                className: "whitespace-nowrap text-xs text-muted-foreground",
                cell: (operation: Operation) =>
                  formatTime(operation.created_at),
              },
              {
                key: "result",
                header: "结果",
                cell: (operation: Operation) => (
                  <div className="flex items-center gap-1">
                    <Button
                      variant="ghost"
                      size="sm"
                      onClick={() =>
                        setTarget({ type: "operation", operation })
                      }
                    >
                      <Eye className="size-3.5" />
                      查看
                    </Button>
                    {["unknown", "running", "queued"].includes(
                      operation.status,
                    ) && (
                      <Button
                        variant="outline"
                        size="sm"
                        loading={pending === operation.id}
                        disabled={Boolean(pending)}
                        onClick={() => void queryReceipt(operation)}
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
      <GroupDialogs target={target} onClose={() => setTarget(null)} />
    </div>
  );
}
