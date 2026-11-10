import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { GroupsPage } from "@/pages/GroupsPage";
import { SettingsPage } from "@/pages/SettingsPage";
import { HistoryPage } from "@/pages/HistoryPage";
import type { useAppData } from "@/app/data-context";
import type { Audit, Config } from "@/lib/types";

const mocks = vi.hoisted(() => ({ useAppData: vi.fn(), api: vi.fn() }));
vi.mock("@/app/data-context", () => ({ useAppData: mocks.useAppData }));
vi.mock("@/lib/api", () => ({ api: mocks.api }));
vi.mock("@/app/theme-provider", () => ({
  useTheme: () => ({ theme: "dark", resolvedTheme: "dark", setTheme: vi.fn() }),
}));
vi.mock("@/features/groups/group-dialogs", () => ({
  GroupDialogs: () => null,
}));

// 以下为隔离单元测试的最小契约输入，不写入应用数据库，也不作为页面演示数据。
const baseConfig: Config = {
  debounce_ms: 1000,
  cooldown_seconds: 30,
  weak_recheck_seconds: 60,
  poll_seconds: 0,
  retention_days: 30,
  groups: [],
};
let context: ReturnType<typeof useAppData>;

beforeEach(() => {
  mocks.api.mockReset();
  context = {
    config: structuredClone(baseConfig),
    status: {
      connected: true,
      connection_error: "",
      config: structuredClone(baseConfig),
      groups: [],
      directory: [],
    },
    accounts: [],
    rooms: [],
    operations: [],
    events: [],
    dashboard: null,
    accountId: "unit-account",
    setAccountId: vi.fn(),
    days: 7,
    setDays: vi.fn(),
    page: "settings",
    navigate: vi.fn(),
    loading: false,
    refreshing: false,
    error: "",
    refresh: vi.fn().mockResolvedValue(undefined),
    persist: vi.fn().mockResolvedValue(undefined),
    notify: vi.fn(),
    notice: null,
    dismissNotice: vi.fn(),
    roomTitle: (room) => room.name || room.room_id,
    accountName: (id) => id,
  };
  mocks.useAppData.mockImplementation(() => context);
});

describe("业务页面保留现有接口语义", () => {
  it("保存偏好草稿时合并最新群配置，后台刷新不覆盖正在输入的数值", async () => {
    const rendered = render(<SettingsPage />);
    fireEvent.change(screen.getByLabelText("日志保留时间"), {
      target: { value: "45" },
    });
    const latestGroups = [
      {
        account_id: "unit-account",
        room_id: "unit-room@chatroom",
        label: "",
        enabled: true,
        notify_join: false,
        notify_leave: false,
        join_template: "",
        leave_template: "",
      },
    ];
    context = { ...context, config: { ...baseConfig, groups: latestGroups } };
    rendered.rerender(<SettingsPage />);
    expect(
      (screen.getByLabelText("日志保留时间") as HTMLInputElement).value,
    ).toBe("45");
    fireEvent.click(screen.getByRole("button", { name: "保存设置" }));
    await waitFor(() =>
      expect(context.persist).toHaveBeenCalledWith({
        ...baseConfig,
        retention_days: 45,
        groups: latestGroups,
      }),
    );
  });

  it("定时检查填写 1 秒时显示行内错误，且不提交配置", () => {
    render(<SettingsPage />);
    fireEvent.change(screen.getByLabelText("定时检查间隔"), {
      target: { value: "1" },
    });
    fireEvent.click(screen.getByRole("button", { name: "保存设置" }));
    expect(screen.getByRole("alert").textContent).toContain("开启时至少 60 秒");
    expect(context.persist).not.toHaveBeenCalled();
    expect(document.activeElement).toBe(screen.getByLabelText("定时检查间隔"));
  });

  it("首次开启群记录时，欢迎消息和离群消息仍默认关闭", async () => {
    context = {
      ...context,
      page: "groups",
      rooms: [
        {
          account_id: "unit-account",
          room_id: "unit-room@chatroom",
          name: "单元测试群",
        },
      ],
    };
    render(<GroupsPage />);
    fireEvent.click(screen.getByRole("button", { name: "开启记录" }));
    await waitFor(() => expect(context.persist).toHaveBeenCalledTimes(1));
    const submitted = vi.mocked(context.persist).mock.calls[0][0];
    expect(submitted.groups).toHaveLength(1);
    expect(submitted.groups[0]).toMatchObject({
      account_id: "unit-account",
      room_id: "unit-room@chatroom",
      enabled: true,
      notify_join: false,
      notify_leave: false,
    });
    expect(mocks.api).not.toHaveBeenCalled();
  });

  it("成员通知查询只携带原审计 ID，不提交新的业务操作", async () => {
    context = { ...context, page: "history" };
    const audit: Audit = {
      id: 41,
      account_id: "unit-account",
      room_id: "unit-room@chatroom",
      revision: 1,
      kind: "joined",
      member_id: "unit-member",
      name: "单元测试成员",
      observed_at: "2026-01-01T00:00:00Z",
      source: "member_snapshot_diff",
      trigger: "hint",
      reason: "",
      before_count: 1,
      after_count: 2,
      gap: false,
      notify_status: "unknown",
      notify_call_id: "original-call",
      notify_error: "",
      inviter_id: "",
      inviter_name: "",
    };
    mocks.api.mockImplementation(async (path: string) =>
      path.startsWith("/api/events?") ? [audit] : { status: "succeeded" },
    );
    render(<HistoryPage />);
    fireEvent.click(await screen.findByRole("button", { name: "查询原结果" }));
    await waitFor(() =>
      expect(mocks.api).toHaveBeenCalledWith("/api/receipt", { id: 41 }),
    );
    expect(
      mocks.api.mock.calls.some(([path]) =>
        String(path).startsWith("/api/operation"),
      ),
    ).toBe(false);
  });

  it("首次加载失败时显示可重试错误，不被 LoadingState 隐藏", () => {
    context = {
      ...context,
      page: "groups",
      status: null,
      loading: true,
      error: "本地服务未响应",
    };
    render(<GroupsPage />);
    expect(screen.getByRole("alert").textContent).toContain("本地服务未响应");
    fireEvent.click(screen.getByRole("button", { name: "重新加载" }));
    expect(context.refresh).toHaveBeenCalledTimes(1);
  });
});
