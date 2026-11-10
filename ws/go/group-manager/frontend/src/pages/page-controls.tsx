import type { ReactNode } from "react";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Button } from "@/components/ui/button";
import type { Period, Room } from "@/lib/types";
import { groupKey } from "@/lib/format";

/** 群筛选始终携带账号，防止同一群在多个微信账号下串用记录。 */
export function RoomFilter({
  value,
  onChange,
  rooms,
  roomTitle,
  accountName,
}: {
  value: string;
  onChange: (value: string) => void;
  rooms: Room[];
  roomTitle: (room: Room) => string;
  accountName: (id: string) => string;
}) {
  return (
    <Select
      value={value || "all"}
      onValueChange={(next) => onChange(next === "all" ? "" : next)}
    >
      <SelectTrigger aria-label="按群聊筛选" className="w-full sm:w-60">
        <SelectValue placeholder="全部群聊" />
      </SelectTrigger>
      <SelectContent>
        <SelectItem value="all">全部群聊</SelectItem>
        {rooms.map((room) => (
          <SelectItem key={groupKey(room)} value={groupKey(room)}>
            <span className="inline-flex max-w-80 flex-col text-left">
              <span className="truncate">{roomTitle(room)}</span>
              <span className="text-xs text-muted-foreground">
                {accountName(room.account_id)}
              </span>
            </span>
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}

export function PeriodFilter({
  days,
  onChange,
}: {
  days: Period;
  onChange: (days: Period) => void;
}) {
  return (
    <div
      className="inline-flex max-w-full items-center gap-0.5 rounded-md bg-secondary p-1"
      role="group"
      aria-label="统计时间范围"
    >
      {([1, 7, 14, 30] as const).map((value) => (
        <Button
          key={value}
          size="sm"
          variant={days === value ? "outline" : "ghost"}
          aria-pressed={days === value}
          onClick={() => onChange(value)}
          className="h-8 px-2 text-xs sm:px-3 sm:text-sm"
        >
          {value === 1 ? "今天" : `近 ${value} 天`}
        </Button>
      ))}
    </div>
  );
}

/** 多行身份单元格统一截断；完整 ID 保留在 title 中，可复制原始文本。 */
export function IdentityCell({
  title,
  detail,
  leading,
}: {
  title: ReactNode;
  detail?: string;
  leading?: ReactNode;
}) {
  return (
    <div className="flex min-w-0 items-center gap-3">
      {leading}
      <div className="min-w-0">
        <div className="max-w-64 truncate font-medium text-foreground">
          {title}
        </div>
        {detail && (
          <div
            className="mt-0.5 max-w-72 truncate text-xs text-muted-foreground"
            title={detail}
          >
            {detail}
          </div>
        )}
      </div>
    </div>
  );
}

export function roomFilterParams(value: string, accountId: string) {
  if (!value) return { account_id: accountId, room_id: "" };
  const [account_id, room_id] = value.split("\0");
  return { account_id, room_id };
}
