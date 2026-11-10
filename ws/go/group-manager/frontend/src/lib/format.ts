import type { Action, Room } from "./types";
export const groupKey = (room: Pick<Room, "account_id" | "room_id">) =>
  `${room.account_id}\0${room.room_id}`;
export function formatTime(value?: string) {
  if (!value || value.startsWith("0001")) return "尚未同步";
  const time = new Date(value);
  return Number.isNaN(+time)
    ? "尚未同步"
    : time.toLocaleString("zh-CN", { hour12: false });
}
export const formatNumber = (value: number) =>
  new Intl.NumberFormat("zh-CN").format(value);
export const actionNames: Record<Action, string> = {
  info: "查看群公告",
  qrcode: "获取群二维码",
  name: "修改群名称",
  announcement: "发布群公告",
  remark: "设置群备注",
  self_name: "设置我的群昵称",
  show_names: "显示群成员昵称",
  save_contact: "保存到通讯录",
  remove_members: "移出群成员",
  add_admin: "添加管理员",
  remove_admin: "移除管理员",
  transfer_owner: "转让群主",
  quit: "退出群聊",
  join_scan: "通过普通群链接入群",
  join_scan_enterprise: "通过企业互通群链接入群",
  accept_invite: "确认本人入群邀请",
};
export const statuses: Record<string, string> = {
  queued: "等待执行",
  running: "正在执行",
  succeeded: "已完成",
  failed: "未成功",
  rejected: "被拒绝",
  cancelled: "已取消",
  unknown: "结果待确认",
  partial: "部分完成",
  disabled: "仅记录",
  pending: "待发送",
  sending: "发送中",
};
export const kinds: Record<string, string> = {
  baseline: "开始记录",
  joined: "成员入群",
  left: "成员离群",
};
