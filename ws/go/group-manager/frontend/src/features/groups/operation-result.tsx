import { useState } from "react";
import { CheckCircle2, Clock3, RefreshCw } from "lucide-react";
import { api } from "@/lib/api";
import type { Operation } from "@/lib/types";
import { formatTime, statuses } from "@/lib/format";
import { useAppData } from "@/app/data-context";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { ErrorState } from "@/components/shared/error-state";

/** 结果不明时仅查询原幂等调用；此组件没有重新执行写操作的入口。 */
export function OperationResult({
  operation,
  onUpdate,
}: {
  operation: Operation;
  onUpdate: (operation: Operation) => void;
}) {
  const { refresh } = useAppData();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const pending = ["queued", "running", "unknown"].includes(operation.status);
  const result = operation.result;
  const buffer = result?.qrcode?.buffer;
  const validImage =
    typeof buffer === "string" &&
    /^[a-zA-Z0-9+/=\s]+$/.test(buffer) &&
    buffer.length < 2000000;
  const query = async () => {
    if (busy) return;
    setBusy(true);
    setError("");
    try {
      const updated = await api<Operation>("/api/operation-receipt", {
        id: operation.id,
      });
      onUpdate(updated);
      await refresh();
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "查询原结果失败");
    } finally {
      setBusy(false);
    }
  };
  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3 rounded-lg bg-muted/40 p-4">
        <Badge
          variant={
            operation.status === "succeeded"
              ? "success"
              : ["failed", "rejected", "partial"].includes(operation.status)
                ? "destructive"
                : "warning"
          }
        >
          {operation.status === "succeeded" ? <CheckCircle2 /> : <Clock3 />}
          {statuses[operation.status] || operation.status}
        </Badge>
        <span className="text-xs text-muted-foreground">
          提交于 {formatTime(operation.created_at)}
        </span>
      </div>
      {operation.detail && (
        <p className="break-words rounded-md bg-destructive/5 p-3 text-sm leading-6 text-destructive">
          {operation.detail}
        </p>
      )}
      {pending && (
        <div className="space-y-3">
          <p className="text-sm leading-6 text-muted-foreground">
            {operation.status === "unknown"
              ? "暂时无法确认微信实际结果。查询会核对原操作，不会重复提交。"
              : "操作正在处理中，结果会自动刷新。"}
          </p>
          <Button variant="outline" onClick={() => void query()} loading={busy}>
            <RefreshCw />
            查询原结果
          </Button>
        </div>
      )}
      {operation.status === "succeeded" &&
        (operation.action === "qrcode" ? (
          <div className="space-y-3 text-center">
            {validImage ? (
              <>
                <img
                  src={`data:image/png;base64,${buffer}`}
                  alt="微信群二维码"
                  className="mx-auto size-60 max-w-full rounded-lg border border-border object-contain"
                />
                <p className="text-xs text-muted-foreground">
                  二维码有效期和入群权限由微信决定。
                </p>
              </>
            ) : (
              <p className="text-sm text-muted-foreground">
                接口已返回，但没有可显示的二维码图像。
              </p>
            )}
          </div>
        ) : operation.action === "info" ? (
          <div className="space-y-4">
            <p className="whitespace-pre-wrap break-words rounded-lg bg-muted/30 p-4 text-sm leading-7">
              {result?.announcement || "当前没有群公告。"}
            </p>
            <div className="space-y-1 text-xs text-muted-foreground">
              {result?.announcementEditor && (
                <p>发布人：{result.announcementEditor}</p>
              )}
              {!!result?.announcementPublishTime && (
                <p>
                  发布时间：
                  {formatTime(
                    new Date(
                      result.announcementPublishTime * 1000,
                    ).toISOString(),
                  )}
                </p>
              )}
            </div>
          </div>
        ) : (
          <p className="text-sm leading-6">
            {result?.room_id
              ? `已加入群聊：${result.room_id}`
              : "微信已返回操作成功。"}
          </p>
        ))}
      {error && <ErrorState message={error} />}
      <p className="break-all border-t border-border pt-3 text-xs text-muted-foreground">
        操作编号 · {operation.id}
      </p>
    </div>
  );
}
