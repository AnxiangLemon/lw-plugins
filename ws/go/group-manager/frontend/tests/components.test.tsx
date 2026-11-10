import { useState } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { DataTable } from "@/components/shared/data-table";
import { ConfirmDialog } from "@/components/shared/confirm-dialog";
import { ThemeProvider, useTheme } from "@/app/theme-provider";

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

// 这里的数据仅是组件名称；测试不构造群聊、成员或业务 API 响应。
const componentRows = ["Button", "Input", "Select", "Dialog", "Separator"].map(
  (name) => ({ name }),
);
const columns = [
  {
    key: "name",
    header: "组件名称",
    cell: (row: { name: string }) => row.name,
  },
];

describe("DataTable 的本地分页", () => {
  it("按已加载数据分页，筛选改变时回第一页，数据缩短时不会停在空页", async () => {
    const user = userEvent.setup();
    const { rerender } = render(
      <DataTable
        rows={componentRows}
        columns={columns}
        rowKey={(row) => row.name}
        pageSize={2}
        pageKey="all"
      />,
    );
    expect(screen.getByText("Button")).toBeTruthy();
    expect(screen.queryByText("Select")).toBeNull();
    expect(
      (screen.getByRole("button", { name: "上一页" }) as HTMLButtonElement)
        .disabled,
    ).toBe(true);
    await user.click(screen.getByRole("button", { name: "下一页" }));
    expect(screen.getByText("Select")).toBeTruthy();
    expect(screen.queryByText("Button")).toBeNull();
    rerender(
      <DataTable
        rows={componentRows.slice(0, 3)}
        columns={columns}
        rowKey={(row) => row.name}
        pageSize={2}
        pageKey="filtered"
      />,
    );
    expect(screen.getByText("Button")).toBeTruthy();
    expect(screen.getByText("1 / 2")).toBeTruthy();
    await user.click(screen.getByRole("button", { name: "下一页" }));
    rerender(
      <DataTable
        rows={componentRows.slice(0, 1)}
        columns={columns}
        rowKey={(row) => row.name}
        pageSize={2}
        pageKey="filtered"
      />,
    );
    expect(screen.getByText("Button")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "下一页" })).toBeNull();
  });
});

describe("Button 状态", () => {
  it("disabled 和 loading 都阻止点击，并为加载提供 aria-busy", async () => {
    const user = userEvent.setup();
    const click = vi.fn();
    const { rerender } = render(
      <Button disabled onClick={click}>
        保存
      </Button>,
    );
    await user.click(screen.getByRole("button", { name: "保存" }));
    expect(click).not.toHaveBeenCalled();
    rerender(
      <Button loading onClick={click}>
        保存
      </Button>,
    );
    const busy = screen.getByRole("button", { name: "保存" });
    expect(busy.getAttribute("aria-busy")).toBe("true");
    await user.click(busy);
    expect(click).not.toHaveBeenCalled();
    rerender(<Button onClick={click}>保存</Button>);
    await user.click(screen.getByRole("button", { name: "保存" }));
    expect(click).toHaveBeenCalledTimes(1);
    rerender(
      <Button asChild disabled>
        <a href="#component" onClick={click}>
          组件详情
        </a>
      </Button>,
    );
    const link = screen.getByRole("link", { name: "组件详情" });
    expect(link.getAttribute("tabindex")).toBe("-1");
    fireEvent.click(link);
    expect(click).toHaveBeenCalledTimes(1);
  });
});

describe("Dialog 的键盘和焦点", () => {
  it("Escape 关闭后归还触发按钮焦点", async () => {
    const user = userEvent.setup();
    render(
      <Dialog>
        <DialogTrigger asChild>
          <Button>打开详情</Button>
        </DialogTrigger>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>组件详情</DialogTitle>
            <DialogDescription>确认焦点正确管理。</DialogDescription>
          </DialogHeader>
          <input aria-label="详情输入" />
        </DialogContent>
      </Dialog>,
    );
    const trigger = screen.getByRole("button", { name: "打开详情" });
    await user.click(trigger);
    expect(screen.getByRole("dialog", { name: "组件详情" })).toBeTruthy();
    await user.keyboard("{Escape}");
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(document.activeElement).toBe(trigger);
  });
});

describe("Select 的选择语义", () => {
  it("键盘可打开菜单，禁用项不可选，选择结果回传调用方", async () => {
    const user = userEvent.setup();
    const changed = vi.fn();
    render(
      <Select defaultValue="dark" onValueChange={changed}>
        <SelectTrigger aria-label="选择外观">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value="dark">深色</SelectItem>
          <SelectItem value="light">浅色</SelectItem>
          <SelectItem value="disabled" disabled>
            不可用
          </SelectItem>
        </SelectContent>
      </Select>,
    );
    screen.getByRole("combobox", { name: "选择外观" }).focus();
    await user.keyboard(" ");
    const light = await screen.findByRole("option", { name: "浅色" });
    expect(
      screen
        .getByRole("option", { name: "不可用" })
        .getAttribute("aria-disabled"),
    ).toBe("true");
    await user.click(light);
    expect(changed).toHaveBeenLastCalledWith("light");
    expect(
      screen.getByRole("combobox", { name: "选择外观" }).textContent,
    ).toContain("浅色");
  });
});

describe("ConfirmDialog 的异步提交", () => {
  it("提交中阻止重复，失败后保留对话框且不自动重试", async () => {
    const user = userEvent.setup();
    let rejectRequest: ((error: Error) => void) | undefined;
    const confirm = vi.fn(
      () =>
        new Promise<void>((_resolve, reject) => {
          rejectRequest = reject;
        }),
    );
    function Example() {
      const [open, setOpen] = useState(true);
      return (
        <ConfirmDialog
          open={open}
          onOpenChange={setOpen}
          title="确认交互"
          description="只验证异步回调，不调用业务接口。"
          onConfirm={confirm}
        />
      );
    }
    render(<Example />);
    await user.dblClick(screen.getByRole("button", { name: "确认提交" }));
    expect(confirm).toHaveBeenCalledTimes(1);
    expect(
      (screen.getByRole("button", { name: "取消" }) as HTMLButtonElement)
        .disabled,
    ).toBe(true);
    await act(async () => {
      rejectRequest?.(new Error("测试回调失败"));
    });
    expect(screen.getByRole("alert").textContent).toContain("测试回调失败");
    expect(screen.getByRole("alertdialog", { name: "确认交互" })).toBeTruthy();
    expect(confirm).toHaveBeenCalledTimes(1);
    expect(
      (screen.getByRole("button", { name: "确认提交" }) as HTMLButtonElement)
        .disabled,
    ).toBe(false);
    await user.click(screen.getByRole("button", { name: "取消" }));
    await waitFor(() => expect(screen.queryByRole("alertdialog")).toBeNull());
  });
});

function ThemeControls() {
  const { theme, resolvedTheme, setTheme } = useTheme();
  return (
    <>
      <output aria-label="主题结果">
        {theme}:{resolvedTheme}
      </output>
      <Button onClick={() => setTheme("dark")}>深色主题</Button>
      <Button onClick={() => setTheme("light")}>浅色主题</Button>
      <Button onClick={() => setTheme("system")}>跟随系统</Button>
    </>
  );
}

describe("ThemeProvider 的选择与持久化", () => {
  it("默认深色，保留手动主题，System 模式实时响应系统变化", async () => {
    const user = userEvent.setup();
    localStorage.removeItem("lw-group-manager-theme");
    const media = Object.assign(new EventTarget(), {
      matches: false,
      media: "(prefers-color-scheme: dark)",
      onchange: null,
      addListener() {},
      removeListener() {},
    }) as unknown as MediaQueryList;
    vi.spyOn(window, "matchMedia").mockReturnValue(media);
    let view = render(
      <ThemeProvider>
        <ThemeControls />
      </ThemeProvider>,
    );
    expect(screen.getByLabelText("主题结果").textContent).toBe("dark:dark");
    expect(document.documentElement.dataset.theme).toBe("dark");
    await user.click(screen.getByRole("button", { name: "浅色主题" }));
    expect(localStorage.getItem("lw-group-manager-theme")).toBe("light");
    expect(document.documentElement.dataset.theme).toBe("light");
    view.unmount();
    view = render(
      <ThemeProvider>
        <ThemeControls />
      </ThemeProvider>,
    );
    expect(screen.getByLabelText("主题结果").textContent).toBe("light:light");
    await user.click(screen.getByRole("button", { name: "跟随系统" }));
    expect(screen.getByLabelText("主题结果").textContent).toBe("system:light");
    act(() => {
      Object.defineProperty(media, "matches", {
        configurable: true,
        value: true,
      });
      media.dispatchEvent(new Event("change"));
    });
    expect(screen.getByLabelText("主题结果").textContent).toBe("system:dark");
    await user.click(screen.getByRole("button", { name: "浅色主题" }));
    act(() => {
      Object.defineProperty(media, "matches", {
        configurable: true,
        value: false,
      });
      media.dispatchEvent(new Event("change"));
    });
    expect(screen.getByLabelText("主题结果").textContent).toBe("light:light");
    view.unmount();
    localStorage.removeItem("lw-group-manager-theme");
  });
});
