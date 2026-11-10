/** 与 Go HTTP 白名单字段保持一致；界面扩展字段绝不直接发送给写接口。 */
export interface Account {
  account_id: string;
  display_name: string;
  online: boolean;
}
export interface Group {
  account_id: string;
  room_id: string;
  label: string;
  enabled: boolean;
  notify_join: boolean;
  notify_leave: boolean;
  join_template: string;
  leave_template: string;
}
export interface Config {
  debounce_ms: number;
  cooldown_seconds: number;
  weak_recheck_seconds: number;
  poll_seconds: number;
  retention_days: number;
  groups: Group[];
}
export interface GroupStatus extends Group {
  pending: boolean;
  running: boolean;
  last_checked: string;
  error: string;
  member_count: number;
  version: number;
}
export type GroupState = GroupStatus;
export interface Room {
  account_id: string;
  room_id: string;
  name?: string;
  remark?: string;
  owner?: string;
  updated_at?: string;
  label?: string;
}
export interface DirectoryStatus {
  account_id: string;
  running: boolean;
  updated_at: string;
  error: string;
  count: number;
}
export interface StatusResponse {
  connected: boolean;
  connection_error: string;
  config: Config;
  groups: GroupStatus[];
  directory: DirectoryStatus[];
}
export interface Member {
  userName: string;
  nickName: string;
  displayName: string;
  inviterUserName?: string;
}
export interface Snapshot {
  server_version: number;
  members: Member[] | null;
}
export interface InviterStat {
  account_id: string;
  room_id: string;
  inviter_id: string;
  name: string;
  current_members: number;
  joined: number;
}
export interface TrendPoint {
  day: string;
  joined: number;
  left: number;
}
export interface Dashboard {
  joined: number;
  left: number;
  members: number;
  known_invite_members: number;
  unknown_invite_members: number;
  days: number;
  trend: TrendPoint[];
  inviters: InviterStat[];
}
export interface Audit {
  id: number;
  account_id: string;
  room_id: string;
  revision: number;
  kind: string;
  member_id: string;
  name: string;
  observed_at: string;
  source: string;
  trigger: string;
  reason: string;
  before_count: number;
  after_count: number;
  gap: boolean;
  notify_status: string;
  notify_call_id: string;
  notify_error: string;
  inviter_id: string;
  inviter_name: string;
}
export type Action =
  | "info"
  | "qrcode"
  | "name"
  | "announcement"
  | "remark"
  | "self_name"
  | "show_names"
  | "save_contact"
  | "remove_members"
  | "add_admin"
  | "remove_admin"
  | "transfer_owner"
  | "quit"
  | "join_scan"
  | "join_scan_enterprise"
  | "accept_invite";
export interface GroupInfo {
  announcement?: string;
  announcementEditor?: string;
  announcementPublishTime?: number;
  chatRoomStatus?: number;
  topic?: string;
}
export interface OperationResult extends GroupInfo {
  qrcode?: { buffer: string };
  qrCode?: { buffer: string };
  qrcodeBuf?: { buffer: string };
  qrCodeBuf?: { buffer: string };
  room_id?: string;
}
export interface OperationInput {
  id: string;
  account_id: string;
  room_id: string;
  action: Action;
  content?: string;
  members?: string[];
  enabled?: boolean;
  url?: string;
}
export interface Operation extends OperationInput {
  status: string;
  call_id: string;
  detail: string;
  result: OperationResult | null;
  created_at: string;
}
export type Page =
  "dashboard" | "groups" | "inviters" | "history" | "operations" | "settings";
export type Period = 1 | 7 | 14 | 30;
