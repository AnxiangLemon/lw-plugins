import { useEffect, useState, type ReactNode } from "react";
import {
  Activity,
  ArrowUpRight,
  Check,
  CheckCircle2,
  ChevronDown,
  CircleHelp,
  Command as CommandIcon,
  LayoutDashboard,
  ListChecks,
  Loader2,
  Menu,
  MessageSquare,
  Monitor,
  Moon,
  PanelLeftClose,
  PanelLeftOpen,
  RefreshCw,
  Search,
  Settings2,
  ShieldCheck,
  Sun,
  UsersRound,
  WifiOff,
  X,
} from "lucide-react";
import { useAppData } from "@/app/data-context";
import { useTheme, type Theme } from "@/app/theme-provider";
import type { Page } from "@/lib/types";
import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Avatar, AvatarFallback } from "@/components/ui/avatar";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from "@/components/ui/command";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";

const navigation = [
  { page: "dashboard" as Page, label: "数据概览", icon: LayoutDashboard },
  { page: "groups" as Page, label: "群聊管理", icon: MessageSquare },
  { page: "inviters" as Page, label: "邀请人统计", icon: UsersRound },
  { page: "history" as Page, label: "成员动态", icon: Activity },
  { page: "operations" as Page, label: "操作记录", icon: ListChecks },
];
const themes = [
  { value: "dark" as Theme, label: "深色", icon: Moon },
  { value: "light" as Theme, label: "浅色", icon: Sun },
  { value: "system" as Theme, label: "跟随系统", icon: Monitor },
];

export function ThemeSwitcher() {
  const { theme, setTheme } = useTheme();
  const Icon = themes.find((item) => item.value === theme)?.icon || Moon;
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="ghost" size="icon" aria-label="切换主题">
          <Icon className="size-4" />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end">
        <DropdownMenuLabel>外观主题</DropdownMenuLabel>
        <DropdownMenuSeparator />
        {themes.map((item) => (
          <DropdownMenuItem
            key={item.value}
            onSelect={() => setTheme(item.value)}
          >
            <item.icon className="size-4" />
            {item.label}
            {theme === item.value && (
              <Check className="ml-auto size-3.5 text-primary" />
            )}
          </DropdownMenuItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
function AccountPicker() {
  const { accounts, accountId, setAccountId, accountName } = useAppData();
  const [open, setOpen] = useState(false);
  const choose = (id: string) => {
    setAccountId(id);
    setOpen(false);
  };
  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button
          variant="ghost"
          className="max-w-52 gap-2 px-2"
          role="combobox"
          aria-expanded={open}
          aria-label="当前微信账号"
        >
          <Avatar className="size-6 rounded-md">
            <AvatarFallback className="rounded-md text-xs">
              {accountId ? (
                accountName(accountId).slice(0, 1)
              ) : (
                <UsersRound className="size-3.5" />
              )}
            </AvatarFallback>
          </Avatar>
          <span className="max-w-32 truncate text-sm">
            {accountId ? accountName(accountId) : "全部微信账号"}
          </span>
          <ChevronDown className="size-3 text-muted-foreground" />
        </Button>
      </PopoverTrigger>
      <PopoverContent className="w-72 p-0" align="end">
        <Command>
          <CommandInput placeholder="搜索微信账号…" />
          <CommandList>
            <CommandEmpty>未找到匹配的账号</CommandEmpty>
            <CommandGroup heading="微信账号">
              <CommandItem value="全部微信账号" onSelect={() => choose("")}>
                <UsersRound className="size-4" />
                全部微信账号
                {!accountId && (
                  <Check className="ml-auto size-3.5 text-primary" />
                )}
              </CommandItem>
              {accounts.map((account) => (
                <CommandItem
                  key={account.account_id}
                  value={`${account.display_name} ${account.account_id}`}
                  onSelect={() => choose(account.account_id)}
                >
                  <Avatar className="size-6 rounded-md">
                    <AvatarFallback className="rounded-md text-xs">
                      {account.display_name.slice(0, 1)}
                    </AvatarFallback>
                  </Avatar>
                  <span className="min-w-0 flex-1 truncate">
                    {account.display_name}
                  </span>
                  <span
                    className={cn(
                      "size-1.5 rounded-full",
                      account.online ? "bg-success" : "bg-muted-foreground",
                    )}
                  />
                  <span className="text-xs text-muted-foreground">
                    {account.online ? "在线" : "离线"}
                  </span>
                  {accountId === account.account_id && (
                    <Check className="size-3.5 text-primary" />
                  )}
                </CommandItem>
              ))}
            </CommandGroup>
          </CommandList>
          {!accounts.length && (
            <p className="border-t px-3 py-2 text-xs text-muted-foreground">
              连接 Desktop 后自动读取微信账号。
            </p>
          )}
        </Command>
      </PopoverContent>
    </Popover>
  );
}
export function AppLayout({ children }: { children: ReactNode }) {
  const {
    page,
    navigate,
    status,
    rooms,
    accountId,
    refreshing,
    refresh,
    notice,
    dismissNotice,
    error,
  } = useAppData();
  const [collapsed, setCollapsed] = useState(() => {
    try {
      return localStorage.getItem("lw-group-manager-sidebar") === "collapsed";
    } catch {
      return false;
    }
  });
  const [mobileOpen, setMobileOpen] = useState(false);
  const [commandOpen, setCommandOpen] = useState(false);
  const toggleSidebar = () =>
    setCollapsed((previous) => {
      try {
        localStorage.setItem(
          "lw-group-manager-sidebar",
          previous ? "expanded" : "collapsed",
        );
      } catch {
        /* 不可写存储时仍保持本次状态。 */
      }
      return !previous;
    });
  const go = (next: Page) => {
    navigate(next);
    setMobileOpen(false);
    setCommandOpen(false);
  };
  useEffect(() => {
    const shortcut = (event: KeyboardEvent) => {
      if ((event.ctrlKey || event.metaKey) && event.key === "k") {
        event.preventDefault();
        setCommandOpen((value) => !value);
      }
    };
    document.addEventListener("keydown", shortcut);
    return () => document.removeEventListener("keydown", shortcut);
  }, []);
  const count = rooms.filter(
    (room) => !accountId || room.account_id === accountId,
  ).length;
  function sidebar(compact = collapsed) {
    const item = (entry: (typeof navigation)[number]) => (
      <Tooltip key={entry.page}>
        <TooltipTrigger asChild>
          <button
            onClick={() => go(entry.page)}
            aria-current={page === entry.page ? "page" : undefined}
            aria-label={compact ? entry.label : undefined}
            className={cn(
              "flex h-[38px] w-full items-center gap-3 rounded-md px-3 text-sm transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
              compact && "justify-center px-0",
              page === entry.page
                ? "bg-accent text-accent-foreground"
                : "text-sidebar-foreground hover:bg-secondary/60 hover:text-foreground",
            )}
          >
            <entry.icon className={cn("size-[18px] shrink-0", page === entry.page && "text-highlight")} strokeWidth={1.75} />
            {!compact && (
              <>
                <span>{entry.label}</span>
                {entry.page === "groups" && (
                  <span className="ml-auto text-xs tabular-nums opacity-65">
                    {count}
                  </span>
                )}
              </>
            )}
          </button>
        </TooltipTrigger>
        {compact && <TooltipContent side="right">{entry.label}</TooltipContent>}
      </Tooltip>
    );
    return (
      <div className="flex h-full min-h-0 flex-col">
        <button
          className={cn(
            "flex h-16 shrink-0 items-center gap-2.5 px-5 text-left",
            compact && "justify-center px-0",
          )}
          onClick={() => go("dashboard")}
          aria-label="微信群管家首页"
        >
          <span className="flex size-8 shrink-0 items-center justify-center rounded-lg bg-highlight/15 text-highlight">
            <MessageSquare className="size-[18px]" strokeWidth={2} />
          </span>
          {!compact && (
            <span className="min-w-0">
              <span className="block text-sm font-semibold tracking-tight text-foreground">
                微信群管家
              </span>
              <span className="text-xs tracking-[.12em] text-muted-foreground">
                LW TOOLS
              </span>
            </span>
          )}
        </button>
        <div className={cn("px-3", compact && "px-2")}>
          <button
            onClick={() => setCommandOpen(true)}
            aria-label="快速导航"
            className={cn(
              "mb-4 flex h-9 w-full items-center gap-2 rounded-md border border-border/70 bg-background/50 px-2.5 text-xs text-muted-foreground hover:text-foreground",
              compact && "justify-center px-0",
            )}
          >
            <Search className="size-3.5" />
            {!compact && (
              <>
                <span>快速导航</span>
                <kbd className="ml-auto text-xs">Ctrl K</kbd>
              </>
            )}
          </button>
          {!compact && (
            <div className="mb-2 px-3 text-xs font-medium tracking-wide text-muted-foreground">
              工作空间
            </div>
          )}
          <nav aria-label="主导航" className="space-y-1">
            {navigation.map(item)}
          </nav>
          <div className="my-4 h-px bg-border/60" />
          {!compact && (
            <div className="mb-2 px-3 text-xs font-medium tracking-wide text-muted-foreground">
              管理
            </div>
          )}
          {item({ page: "settings", label: "偏好设置", icon: Settings2 })}
        </div>
        <div className={cn("mt-auto px-5 pb-4 pt-5", compact && "px-3")}>
          <div
            className={cn(
              "mb-4 flex items-center gap-2 text-xs text-muted-foreground",
              compact && "justify-center",
            )}
          >
            <ShieldCheck className="size-4 shrink-0 text-muted-foreground" />
            {!compact && <span>数据保存在本机</span>}
          </div>
          <div
            className={cn(
              "flex items-center justify-between border-t border-border/70 pt-3",
              compact && "justify-center",
            )}
          >
            {!compact && (
              <span className="text-xs text-muted-foreground">
                本地工作空间
              </span>
            )}
            <Button
              variant="ghost"
              size="icon-sm"
              onClick={toggleSidebar}
              className="hidden md:inline-flex"
              aria-label={compact ? "展开侧边栏" : "收起侧边栏"}
            >
              {compact ? (
                <PanelLeftOpen className="size-4" />
              ) : (
                <PanelLeftClose className="size-4" />
              )}
            </Button>
          </div>
        </div>
      </div>
    );
  }
  return (
    <div className="min-h-dvh bg-background">
      <a
        href="#main-content"
        onClick={(event) => {
          event.preventDefault();
          document.getElementById("main-content")?.focus();
        }}
        className="sr-only focus:not-sr-only focus:fixed focus:left-4 focus:top-4 focus:z-[100] focus:rounded-md focus:bg-primary focus:px-4 focus:py-2 focus:text-primary-foreground"
      >
        跳转到页面内容
      </a>
      <aside
        className={cn(
          "fixed inset-y-0 left-0 z-30 hidden border-r border-border/70 bg-sidebar md:block",
          collapsed ? "w-16" : "w-56",
        )}
      >
        {sidebar()}
      </aside>
      <Sheet open={mobileOpen} onOpenChange={setMobileOpen}>
        <SheetContent side="left" className="w-64 bg-sidebar p-0">
          <SheetHeader className="sr-only">
            <SheetTitle>导航菜单</SheetTitle>
            <SheetDescription>选择需要打开的工作页面</SheetDescription>
          </SheetHeader>
          {sidebar(false)}
        </SheetContent>
      </Sheet>
      <div className={cn("min-w-0", collapsed ? "md:pl-16" : "md:pl-56")}>
        <header className="sticky top-0 z-20 flex h-14 items-center justify-between gap-2 border-b border-border/70 bg-background px-4 md:px-6">
          <div className="flex min-w-0 items-center gap-2">
            <Button
              variant="ghost"
              size="icon"
              className="md:hidden"
              aria-label="打开导航菜单"
              onClick={() => setMobileOpen(true)}
            >
              <Menu className="size-4" />
            </Button>
            <span className="hidden text-xs text-muted-foreground sm:inline">
              工作空间
            </span>
            <span className="hidden text-xs text-border sm:inline">/</span>
            <span className="truncate text-xs font-medium">
              {navigation.find((item) => item.page === page)?.label ||
                "偏好设置"}
            </span>
          </div>
          <div className="flex shrink-0 items-center gap-1 sm:gap-3">
            <span className="hidden items-center gap-1.5 text-xs text-muted-foreground lg:flex">
              <span
                className={cn(
                  "size-1.5 rounded-full",
                  status?.connected ? "bg-success" : "bg-warning",
                )}
              />
              {status?.connected ? "Desktop 已连接" : "等待 Desktop"}
            </span>
            <ThemeSwitcher />
            <span className="h-4 w-px bg-border" />
            <AccountPicker />
          </div>
        </header>
        <main
          id="main-content"
          tabIndex={-1}
          className="mx-auto max-w-[1680px] px-4 py-5 sm:px-6"
        >
          {status && !status.connected && (
            <div className="mb-5 flex items-start gap-3 rounded-lg border border-warning/20 bg-warning/5 px-4 py-3 text-xs">
              <WifiOff className="mt-0.5 size-4 shrink-0 text-warning" />
              <div className="min-w-0">
                <p className="font-medium text-foreground">等待连接 Desktop</p>
                <p className="mt-0.5 break-all text-muted-foreground">
                  {status.connection_error ||
                    "将 plugin-endpoint.json 放在程序旁边，启动后即可自动连接。"}
                </p>
              </div>
              <Button
                variant="ghost"
                size="sm"
                className="ml-auto shrink-0 text-xs"
                onClick={() => go("settings")}
              >
                查看设置
                <ArrowUpRight className="size-3" />
              </Button>
            </div>
          )}
          {error && status && (
            <div
              role="alert"
              className="mb-5 flex flex-wrap items-center gap-2 rounded-md border border-destructive/20 bg-destructive/5 px-4 py-2 text-xs text-destructive"
            >
              <CircleHelp className="size-4" />
              <span className="min-w-0 flex-1 break-words">
                部分数据更新失败：{error}
              </span>
              <Button variant="ghost" size="sm" onClick={() => void refresh()}>
                重试
              </Button>
            </div>
          )}
          {children}
          <footer className="mt-5 flex items-center justify-between border-t border-border/70 pt-4 text-xs text-muted-foreground">
            <span>
              LW Tools <span className="mx-1.5 opacity-40">/</span> 微信群管家
            </span>
            <span className="flex items-center gap-1.5">
              {refreshing ? (
                <Loader2 className="size-3 animate-spin" />
              ) : (
                <CheckCircle2 className="size-3" />
              )}
              {refreshing ? "正在更新" : "本机数据"}
              <button
                aria-label="刷新数据"
                className="ml-1 rounded p-1 hover:bg-accent"
                onClick={() => void refresh()}
                disabled={refreshing}
              >
                <RefreshCw className="size-3" />
              </button>
            </span>
          </footer>
        </main>
      </div>
      {notice && (
        <div
          role={notice.type === "error" ? "alert" : "status"}
          className="fixed bottom-5 left-4 right-4 z-[80] mx-auto flex max-w-md items-center gap-3 rounded-lg border border-border bg-popover px-4 py-3 text-sm text-popover-foreground shadow-popover"
        >
          {notice.type === "error" ? (
            <CircleHelp className="size-4 shrink-0 text-destructive" />
          ) : (
            <CheckCircle2 className="size-4 shrink-0 text-muted-foreground" />
          )}
          <span className="min-w-0 flex-1 break-words">{notice.message}</span>
          <Button
            size="icon-sm"
            variant="ghost"
            onClick={dismissNotice}
            aria-label="关闭提示"
          >
            <X className="size-3.5" />
          </Button>
        </div>
      )}
      <Dialog open={commandOpen} onOpenChange={setCommandOpen}>
        <DialogContent
          className="gap-0 overflow-hidden p-0"
          showCloseButton={false}
        >
          <DialogHeader className="sr-only">
            <DialogTitle>快速导航</DialogTitle>
            <DialogDescription>输入页面名称，按 Enter 打开</DialogDescription>
          </DialogHeader>
          <Command>
            <CommandInput placeholder="前往哪个页面？" />
            <CommandList>
              <CommandEmpty>没有找到这个页面</CommandEmpty>
              <CommandGroup heading="工作空间">
                {[
                  ...navigation,
                  {
                    page: "settings" as Page,
                    label: "偏好设置",
                    icon: Settings2,
                  },
                ].map((item) => (
                  <CommandItem key={item.page} onSelect={() => go(item.page)}>
                    <item.icon className="size-4" />
                    {item.label}
                    {item.page === page && (
                      <Badge variant="secondary" className="ml-auto">
                        当前页面
                      </Badge>
                    )}
                  </CommandItem>
                ))}
              </CommandGroup>
            </CommandList>
            <div className="flex items-center gap-2 border-t p-3 text-xs text-muted-foreground">
              <CommandIcon className="size-3" />
              方向键选择 · Enter 打开 · Esc 关闭
            </div>
          </Command>
        </DialogContent>
      </Dialog>
    </div>
  );
}

