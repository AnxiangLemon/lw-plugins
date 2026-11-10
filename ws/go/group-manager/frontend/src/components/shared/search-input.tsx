import type { ComponentProps } from "react";
import { Search, X } from "lucide-react";
import { cn } from "@/lib/utils";
import { Input } from "@/components/ui/input";
export interface SearchInputProps extends Omit<
  ComponentProps<"input">,
  "onChange" | "value" | "type"
> {
  value: string;
  onChange: (value: string) => void;
}
/** 值保持受控；清空使用同一回调，让搜索、分页和 URL 状态由页面统一管理。 */
export function SearchInput({
  value,
  onChange,
  placeholder = "搜索…",
  className,
  disabled,
  ...props
}: SearchInputProps) {
  return (
    <div className={cn("relative w-full min-w-0 sm:w-72", className)}>
      <Search
        className="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground"
        aria-hidden="true"
      />
      <Input
        {...props}
        disabled={disabled}
        type="search"
        value={value}
        onChange={(event) => onChange(event.target.value)}
        placeholder={placeholder}
        aria-label={props["aria-label"] || placeholder}
        className="pl-9 pr-8 [&::-webkit-search-cancel-button]:hidden"
      />
      {value && (
        <button
          type="button"
          disabled={disabled}
          aria-label="清空搜索"
          onClick={() => onChange("")}
          className="absolute right-2 top-1/2 -translate-y-1/2 rounded p-1 text-muted-foreground outline-none hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring disabled:opacity-45"
        >
          <X className="size-3.5" />
        </button>
      )}
    </div>
  );
}
