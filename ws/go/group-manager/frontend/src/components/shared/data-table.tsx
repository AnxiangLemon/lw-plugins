import { useState, type ReactNode } from "react";
import { ChevronLeft, ChevronRight } from "lucide-react";
import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import { EmptyState } from "./empty-state";
export interface DataTableColumn<T> {
  key: string;
  header: ReactNode;
  cell: (row: T) => ReactNode;
  className?: string;
}
export interface DataTableProps<T> {
  columns: DataTableColumn<T>[];
  rows: T[];
  rowKey: (row: T) => string;
  empty?: ReactNode;
  pageSize?: number;
  pageKey?: string;
  className?: string;
  caption?: string;
}
/**
 * 轻量泛型表格：数据、列与行标识由业务层提供，不读取接口，也不修改传入数组。
 * 分页仅作用于已经加载的 rows；底栏明确标注“已加载”，不会把本地分页冒充服务端总量。
 * pageKey 在账号/筛选变化时切换，让页码回到第一页；数据减少时自动夹到有效页。
 */
export function DataTable<T>({
  columns,
  rows,
  rowKey,
  empty,
  pageSize = 15,
  pageKey = "",
  className,
  caption,
}: DataTableProps<T>) {
  const [pagination, setPagination] = useState({ key: pageKey, page: 1 });
  const size =
    pageSize > 0 ? Math.max(1, Math.floor(pageSize)) : Math.max(1, rows.length);
  const pages = Math.max(1, Math.ceil(rows.length / size));
  const page = Math.min(
    pages,
    pagination.key === pageKey ? pagination.page : 1,
  );
  const start = (page - 1) * size;
  const visibleRows = rows.slice(start, start + size);
  if (!rows.length)
    return (
      <div className={className}>
        {empty ?? (
          <EmptyState compact title="暂无记录" description="有数据后会显示在这里。" />
        )}
      </div>
    );
  return (
    <div className={cn("min-w-0", className)}>
      <div className="w-full overflow-x-auto">
        <table className="w-full border-collapse text-left text-sm">
          {caption && <caption className="sr-only">{caption}</caption>}
          <thead>
            <tr className="border-b border-border bg-muted/30">
              {columns.map((column) => (
                <th
                  key={column.key}
                  scope="col"
                  className={cn(
                    "h-10 whitespace-nowrap px-4 text-sm font-medium text-muted-foreground",
                    column.className,
                  )}
                >
                  {column.header}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {visibleRows.map((row) => (
              <tr
                key={rowKey(row)}
                className="border-b border-border/60 transition-colors last:border-b-0 hover:bg-accent/45"
              >
                {columns.map((column) => (
                  <td
                    key={column.key}
                    className={cn(
                      "px-4 py-2.5 align-middle",
                      column.className,
                    )}
                  >
                    {column.cell(row)}
                  </td>
                ))}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      {pageSize > 0 && rows.length > size && (
        <div className="flex flex-wrap items-center justify-between gap-2 border-t border-border px-4 py-2.5">
          <p className="text-xs text-muted-foreground">
            已加载 {rows.length} 条 · 显示 {start + 1}–
            {Math.min(start + size, rows.length)} 条
          </p>
          <nav aria-label="表格分页" className="flex items-center gap-2">
            <Button
              variant="outline"
              size="icon-sm"
              disabled={page === 1}
              aria-label="上一页"
              onClick={() => setPagination({ key: pageKey, page: page - 1 })}
            >
              <ChevronLeft />
            </Button>
            <span className="min-w-12 text-center text-xs tabular-nums text-muted-foreground">
              {page} / {pages}
            </span>
            <Button
              variant="outline"
              size="icon-sm"
              disabled={page === pages}
              aria-label="下一页"
              onClick={() => setPagination({ key: pageKey, page: page + 1 })}
            >
              <ChevronRight />
            </Button>
          </nav>
        </div>
      )}
    </div>
  );
}
