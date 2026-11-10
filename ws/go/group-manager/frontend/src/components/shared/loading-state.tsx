import { LoaderCircle } from "lucide-react";
import { cn } from "@/lib/utils";
export interface LoadingStateProps {
  title?: string;
  description?: string;
  className?: string;
}
export function LoadingState({
  title = "正在加载",
  description,
  className,
}: LoadingStateProps) {
  return (
    <div
      role="status"
      aria-live="polite"
      className={cn(
        "flex min-h-32 flex-col items-center justify-center gap-3 px-4 py-5 text-center",
        className,
      )}
    >
      <LoaderCircle
        className="size-5 animate-spin text-muted-foreground"
        aria-hidden="true"
      />
      <p className="text-sm text-muted-foreground">{title}</p>
      {description && (
        <p className="max-w-sm text-xs text-muted-foreground">{description}</p>
      )}
    </div>
  );
}
