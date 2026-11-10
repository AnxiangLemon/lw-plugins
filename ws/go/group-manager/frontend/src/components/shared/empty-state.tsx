import type { ReactNode } from "react";
import { Inbox, type LucideIcon } from "lucide-react";
import { cn } from "@/lib/utils";
export interface EmptyStateProps {
  title: string;
  description?: string;
  action?: ReactNode;
  icon?: LucideIcon;
  /** 紧凑空态用于表格和概览：横向排列，避免无数据时撑开整张卡片。 */
  compact?: boolean;
  className?: string;
}
/** 空态描述缺少的真实数据，并可由页面提供下一步；组件不生成演示数据。 */
export function EmptyState({
  title,
  description,
  action,
  icon: Icon = Inbox,
  compact = false,
  className,
}: EmptyStateProps) {
  return (
    <div
      className={cn(
        "flex min-h-36 flex-col items-center justify-center gap-2.5 px-4 py-6 text-center",
        compact && "min-h-24 flex-row flex-wrap gap-3 py-4 text-left",
        className,
      )}
    >
      <span className="flex size-9 shrink-0 items-center justify-center rounded-lg bg-highlight/10 text-highlight">
        <Icon className="size-5" aria-hidden="true" />
      </span>
      <div className="max-w-sm space-y-1">
        <h3 className="text-sm font-medium text-foreground">{title}</h3>
        {description && (
          <p className="text-xs leading-relaxed text-muted-foreground">
            {description}
          </p>
        )}
      </div>
      {action && <div className="mt-1">{action}</div>}
    </div>
  );
}
