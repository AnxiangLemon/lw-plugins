import type { ComponentProps } from "react";
import { Dialog as SheetPrimitive } from "radix-ui";
import { X } from "lucide-react";
import { cn } from "@/lib/utils";
export const Sheet = SheetPrimitive.Root;
export const SheetTrigger = SheetPrimitive.Trigger;
export const SheetClose = SheetPrimitive.Close;
export const SheetPortal = SheetPrimitive.Portal;
export function SheetOverlay({
  className,
  ...props
}: ComponentProps<typeof SheetPrimitive.Overlay>) {
  return (
    <SheetPrimitive.Overlay
      className={cn("fixed inset-0 z-50 bg-overlay/70", className)}
      {...props}
    />
  );
}
/** Sheet 与 Dialog 共享焦点及键盘语义，只改变停靠方向；移动导航使用 left。 */
export function SheetContent({
  className,
  children,
  side = "right",
  ...props
}: ComponentProps<typeof SheetPrimitive.Content> & {
  side?: "left" | "right" | "top" | "bottom";
}) {
  const placement = {
    left: "inset-y-0 left-0 h-full w-[min(85vw,24rem)] border-r",
    right: "inset-y-0 right-0 h-full w-[min(85vw,28rem)] border-l",
    top: "inset-x-0 top-0 max-h-[85dvh] border-b",
    bottom: "inset-x-0 bottom-0 max-h-[85dvh] border-t",
  }[side];
  return (
    <SheetPortal>
      <SheetOverlay />
      <SheetPrimitive.Content
        className={cn(
          "fixed z-50 flex flex-col gap-5 overflow-y-auto border-border bg-popover p-6 text-popover-foreground outline-none",
          placement,
          className,
        )}
        {...props}
      >
        {children}
        <SheetPrimitive.Close
          aria-label="关闭面板"
          className="absolute right-4 top-4 rounded-md p-1.5 text-muted-foreground outline-none hover:bg-accent hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring"
        >
          <X className="size-4" />
        </SheetPrimitive.Close>
      </SheetPrimitive.Content>
    </SheetPortal>
  );
}
export function SheetHeader({ className, ...props }: ComponentProps<"div">) {
  return (
    <div className={cn("flex flex-col gap-1.5 pr-6", className)} {...props} />
  );
}
export function SheetFooter({ className, ...props }: ComponentProps<"div">) {
  return (
    <div className={cn("mt-auto flex flex-col gap-2", className)} {...props} />
  );
}
export function SheetTitle({
  className,
  ...props
}: ComponentProps<typeof SheetPrimitive.Title>) {
  return (
    <SheetPrimitive.Title
      className={cn("text-base font-semibold", className)}
      {...props}
    />
  );
}
export function SheetDescription({
  className,
  ...props
}: ComponentProps<typeof SheetPrimitive.Description>) {
  return (
    <SheetPrimitive.Description
      className={cn("text-sm text-muted-foreground", className)}
      {...props}
    />
  );
}
