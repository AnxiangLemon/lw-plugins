import type { ComponentProps } from "react";
import { Avatar as AvatarPrimitive } from "radix-ui";
import { cn } from "@/lib/utils";
export function Avatar({
  className,
  ...props
}: ComponentProps<typeof AvatarPrimitive.Root>) {
  return (
    <AvatarPrimitive.Root
      className={cn(
        "relative inline-flex size-8 shrink-0 overflow-hidden rounded-md",
        className,
      )}
      {...props}
    />
  );
}
export function AvatarImage({
  className,
  ...props
}: ComponentProps<typeof AvatarPrimitive.Image>) {
  return (
    <AvatarPrimitive.Image
      className={cn("size-full object-cover", className)}
      {...props}
    />
  );
}
/** 头像缺失时展示姓名首字，不用伪造的网络头像或随机用户图片。 */
export function AvatarFallback({
  className,
  ...props
}: ComponentProps<typeof AvatarPrimitive.Fallback>) {
  return (
    <AvatarPrimitive.Fallback
      className={cn(
        "flex size-full items-center justify-center rounded-md bg-secondary text-xs font-semibold text-muted-foreground",
        className,
      )}
      {...props}
    />
  );
}
