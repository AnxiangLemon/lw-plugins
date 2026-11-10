import { useState, type ReactNode } from "react";
import { CircleAlert } from "lucide-react";
import {
  AlertDialog,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Button } from "@/components/ui/button";
export interface ConfirmDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: string;
  description: string;
  children?: ReactNode;
  onConfirm: () => void | Promise<void>;
  loading?: boolean;
  destructive?: boolean;
  confirmLabel?: string;
  cancelLabel?: string;
}
/**
 * 统一确认交互。onConfirm 由页面传入；组件不生成幂等键、不调用 API、不自行重试。
 * 异步失败保留当前确认内容并展示错误，避免客户在不知道结果时误提交新的操作。
 */
export function ConfirmDialog({
  open,
  onOpenChange,
  title,
  description,
  children,
  onConfirm,
  loading = false,
  destructive = false,
  confirmLabel = "确认提交",
  cancelLabel = "取消",
}: ConfirmDialogProps) {
  const [pending, setPending] = useState(false);
  const [error, setError] = useState("");
  const busy = loading || pending;
  function changeOpen(next: boolean) {
    if (busy) return;
    if (!next) setError("");
    onOpenChange(next);
  }
  async function confirm() {
    if (busy) return;
    setPending(true);
    setError("");
    try {
      await onConfirm();
      onOpenChange(false);
    } catch (cause) {
      setError(
        cause instanceof Error ? cause.message : "操作未完成，请稍后再试。",
      );
    } finally {
      setPending(false);
    }
  }
  return (
    <AlertDialog open={open} onOpenChange={changeOpen}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle className="flex items-center gap-2">
            {destructive && <CircleAlert className="size-4 text-destructive" />}
            {title}
          </AlertDialogTitle>
          <AlertDialogDescription>{description}</AlertDialogDescription>
        </AlertDialogHeader>
        {children}
        {error && (
          <p
            role="alert"
            className="rounded-md bg-destructive/10 p-3 text-sm text-destructive"
          >
            {error}
          </p>
        )}
        <AlertDialogFooter>
          <AlertDialogCancel asChild>
            <Button variant="outline" disabled={busy}>
              {cancelLabel}
            </Button>
          </AlertDialogCancel>
          <Button
            variant={destructive ? "destructive" : "default"}
            loading={busy}
            onClick={() => void confirm()}
          >
            {confirmLabel}
          </Button>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}
