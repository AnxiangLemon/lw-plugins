import { CircleAlert, RefreshCw } from "lucide-react";
import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/button";
export interface ErrorStateProps {
  message: string;
  onRetry?: () => void;
  className?: string;
}
export function ErrorState({ message, onRetry, className }: ErrorStateProps) {
  return (
    <div
      role="alert"
      className={cn(
        "flex min-h-32 flex-col items-center justify-center gap-3 px-4 py-5 text-center",
        className,
      )}
    >
      <CircleAlert className="size-5 text-destructive" aria-hidden="true" />
      <p className="max-w-md break-words text-sm leading-relaxed text-muted-foreground">
        {message}
      </p>
      {onRetry && (
        <Button variant="outline" size="sm" onClick={onRetry}>
          <RefreshCw />
          重新加载
        </Button>
      )}
    </div>
  );
}
