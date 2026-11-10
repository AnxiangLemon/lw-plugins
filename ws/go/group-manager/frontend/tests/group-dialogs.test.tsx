import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { GroupDialogs } from "@/features/groups/group-dialogs";
import type { useAppData } from "@/app/data-context";
import type { Config, Operation, Room } from "@/lib/types";

const mocks = vi.hoisted(() => ({ api: vi.fn(), useAppData: vi.fn() }));
vi.mock("@/lib/api", () => ({ api: mocks.api }));
vi.mock("@/app/data-context", () => ({ useAppData: mocks.useAppData }));

// 仅用于隔离契约测试；请求全部被 mock，不读取宿主、不写数据库，也不用于 UI 演示。
const room: Room = {
  account_id: "unit-account",
  room_id: "unit-room@chatroom",
  name: "单元测试群",
};
const config: Config = {
  debounce_ms: 1000,
  cooldown_seconds: 30,
  weak_recheck_seconds: 60,
  poll_seconds: 0,
  retention_days: 30,
  groups: [],
};
const originalOperation: Operation = {
  id: "11111111-1111-4111-8111-111111111111",
  account_id: room.account_id,
  room_id: room.room_id,
  action: "quit",
  status: "unknown",
  call_id: "original-call",
  detail: "",
  result: {},
  created_at: "2026-01-01T00:00:00Z",
};
let context: ReturnType<typeof useAppData>;

beforeEach(() => {
  mocks.api.mockReset();
  context = {
    status: {
      connected: true,
      connection_error: "",
      config,
      groups: [],
      directory: [],
    },
    config,
    accounts: [],
    rooms: [room],
    operations: [],
    events: [],
    dashboard: null,
    accountId: room.account_id,
    setAccountId: vi.fn(),
    days: 7,
    setDays: vi.fn(),
    page: "groups",
    navigate: vi.fn(),
    loading: false,
    refreshing: false,
    error: "",
    refresh: vi.fn().mockResolvedValue(undefined),
    persist: vi.fn().mockResolvedValue(undefined),
    notify: vi.fn(),
    notice: null,
    dismissNotice: vi.fn(),
    roomTitle: (value) => value.name || value.room_id,
    accountName: (id) => id,
  };
  mocks.useAppData.mockImplementation(() => context);
});
afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

describe("群管理弹窗的业务契约", () => {
  it("当前公告读取失败时，不显示可提交的空白公告编辑器", async () => {
    const user = userEvent.setup();
    mocks.api.mockRejectedValue(new Error("读取当前公告失败，请稍后重试"));
    render(<GroupDialogs target={{ type: "group", room }} onClose={vi.fn()} />);
    await user.click(screen.getByRole("button", { name: /^发布群公告/ }));
    const failure = await screen.findByRole("alert");
    expect(failure.textContent).toContain("读取当前公告失败");
    expect(mocks.api).toHaveBeenCalledExactlyOnceWith("/api/group-info", {
      account_id: room.account_id,
      room_id: room.room_id,
    });
    expect(screen.queryByRole("textbox", { name: "发布群公告" })).toBeNull();
    expect(screen.queryByRole("button", { name: "核对并继续" })).toBeNull();
    expect(screen.getByRole("button", { name: "重新加载" })).toBeTruthy();
    expect(
      mocks.api.mock.calls.some(([path]) => path === "/api/operation"),
    ).toBe(false);
  });

  it("结果待确认时只查询原 operation-receipt，不提交新业务操作", async () => {
    const user = userEvent.setup();
    mocks.api.mockResolvedValue({ ...originalOperation, status: "succeeded" });
    render(
      <GroupDialogs
        target={{ type: "operation", operation: originalOperation }}
        onClose={vi.fn()}
      />,
    );
    expect(mocks.api).not.toHaveBeenCalled();
    await user.click(screen.getByRole("button", { name: "查询原结果" }));
    await waitFor(() =>
      expect(mocks.api).toHaveBeenCalledExactlyOnceWith(
        "/api/operation-receipt",
        { id: originalOperation.id },
      ),
    );
    expect(await screen.findByText("微信已返回操作成功。")).toBeTruthy();
    expect(
      mocks.api.mock.calls.some(([path]) => path === "/api/operation"),
    ).toBe(false);
    expect(context.refresh).toHaveBeenCalledTimes(1);
  });

  it("同一次确认提交失败后，手动重试仍使用原 UUID，且不会自动重发", async () => {
    const user = userEvent.setup();
    const uuid = vi
      .spyOn(crypto, "randomUUID")
      .mockReturnValue(
        originalOperation.id as ReturnType<Crypto["randomUUID"]>,
      );
    mocks.api
      .mockRejectedValueOnce(new Error("测试网络超时，结果尚未确认"))
      .mockResolvedValueOnce({ ...originalOperation, status: "queued" });
    render(<GroupDialogs target={{ type: "group", room }} onClose={vi.fn()} />);
    await user.click(screen.getByRole("button", { name: /^退出群聊/ }));
    expect(
      screen.getByRole("alertdialog", { name: "确认退出群聊" }),
    ).toBeTruthy();
    expect(mocks.api).not.toHaveBeenCalled();
    await user.click(screen.getByRole("button", { name: "确认提交" }));
    expect((await screen.findByRole("alert")).textContent).toContain(
      "测试网络超时",
    );
    expect(mocks.api).toHaveBeenCalledTimes(1);
    expect(
      screen.getByRole("alertdialog", { name: "确认退出群聊" }),
    ).toBeTruthy();
    expect(
      (screen.getByRole("button", { name: "确认提交" }) as HTMLButtonElement)
        .disabled,
    ).toBe(false);
    await user.click(screen.getByRole("button", { name: "确认提交" }));
    await waitFor(() => expect(mocks.api).toHaveBeenCalledTimes(2));
    expect(uuid).toHaveBeenCalledTimes(1);
    expect(mocks.api.mock.calls[0]).toEqual(mocks.api.mock.calls[1]);
    expect(mocks.api.mock.calls[0]).toEqual([
      "/api/operation",
      {
        id: originalOperation.id,
        account_id: room.account_id,
        room_id: room.room_id,
        action: "quit",
      },
    ]);
    await waitFor(() => expect(screen.queryByRole("alertdialog")).toBeNull());
    expect(screen.getByText("等待执行")).toBeTruthy();
  });
});
