import { useState } from "react";
import {
  Bell,
  Check,
  Copy,
  MessageSquare,
  Settings,
  Sparkles,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { Badge } from "@/components/ui/badge";
import { Avatar, AvatarFallback } from "@/components/ui/avatar";
import { Checkbox } from "@/components/ui/checkbox";
import { Switch } from "@/components/ui/switch";
import { Separator } from "@/components/ui/separator";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
  SheetTrigger,
} from "@/components/ui/sheet";
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from "@/components/ui/command";
import { DataTable } from "@/components/shared/data-table";
import { PageHeader } from "@/components/shared/page-header";
import { SearchInput } from "@/components/shared/search-input";
import { EmptyState } from "@/components/shared/empty-state";
import { ErrorState } from "@/components/shared/error-state";
import { LoadingState } from "@/components/shared/loading-state";
import { ConfirmDialog } from "@/components/shared/confirm-dialog";

/**
 * 仅供开发环境挂载的组件手册。这里展示组件名与交互，不提供模拟群聊、成员或运营数据。
 * 每个示例都沿用正式页面相同的组件，因此切换主题后也可检查浮层、表单及状态颜色。
 */
export default function ComponentExamples() {
  const [query, setQuery] = useState("");
  const [checked, setChecked] = useState(false);
  const [enabled, setEnabled] = useState(false);
  const [choice, setChoice] = useState("default");
  const [confirmOpen, setConfirmOpen] = useState(false);
  const [confirmed, setConfirmed] = useState(false);
  const inventory = [
    { name: "Button", purpose: "按钮、禁用及加载状态" },
    { name: "Select", purpose: "键盘选择与主题浮层" },
    { name: "DataTable", purpose: "泛型列与本地分页" },
    { name: "ConfirmDialog", purpose: "异步确认与错误反馈" },
  ];
  return (
    <TooltipProvider>
      <div className="space-y-5">
        <PageHeader
          title="组件使用示例"
          description="开发工具 · 所有示例都不会调用业务 API。"
        />
        <section className="space-y-4 rounded-lg border border-border bg-card p-5">
          <h2 className="text-sm font-semibold">按钮与状态</h2>
          <div className="flex flex-wrap items-center gap-2">
            <Button>
              <Check />
              主要按钮
            </Button>
            <Button variant="secondary">次要按钮</Button>
            <Button variant="outline">描边按钮</Button>
            <Button variant="ghost">轻量按钮</Button>
            <Button variant="destructive">危险操作</Button>
            <Button disabled>不可用</Button>
            <Button loading>提交中</Button>
            <Tooltip>
              <TooltipTrigger asChild>
                <Button variant="ghost" size="icon" aria-label="按钮提示">
                  <Settings />
                </Button>
              </TooltipTrigger>
              <TooltipContent>图标按钮应始终提供可访问名称</TooltipContent>
            </Tooltip>
          </div>
          <div className="flex flex-wrap items-center gap-2">
            <Badge>默认标签</Badge>
            <Badge variant="success">成功</Badge>
            <Badge variant="warning">待确认</Badge>
            <Badge variant="destructive">错误</Badge>
            <Badge variant="outline">辅助信息</Badge>
            <Avatar>
              <AvatarFallback>群</AvatarFallback>
            </Avatar>
          </div>
        </section>
        <section className="space-y-4 rounded-lg border border-border bg-card p-5">
          <h2 className="text-sm font-semibold">表单控件</h2>
          <div className="grid gap-4 sm:grid-cols-2">
            <label className="space-y-1.5 text-xs text-muted-foreground">
              文本输入
              <Input placeholder="请输入内容" />
            </label>
            <label className="space-y-1.5 text-xs text-muted-foreground">
              禁用输入
              <Input disabled placeholder="当前不可编辑" />
            </label>
            <div className="space-y-1.5">
              <p
                id="example-select-label"
                className="text-xs text-muted-foreground"
              >
                下拉选择
              </p>
              <Select value={choice} onValueChange={setChoice}>
                <SelectTrigger
                  aria-labelledby="example-select-label"
                  className="w-full"
                >
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="default">默认选项</SelectItem>
                  <SelectItem value="alternative">另一个选项</SelectItem>
                  <SelectItem value="disabled" disabled>
                    不可用选项
                  </SelectItem>
                </SelectContent>
              </Select>
            </div>
            <SearchInput
              value={query}
              onChange={setQuery}
              placeholder="搜索组件名称"
              className="self-end sm:w-full"
            />
          </div>
          <label className="block space-y-1.5 text-xs text-muted-foreground">
            多行输入
            <Textarea placeholder="支持多行说明" />
          </label>
          <div className="flex flex-wrap items-center gap-6">
            <label className="flex items-center gap-2 text-sm">
              <Checkbox
                checked={checked}
                onCheckedChange={(value) => setChecked(value === true)}
              />
              选择此项
            </label>
            <label className="flex items-center gap-2 text-sm">
              <Switch checked={enabled} onCheckedChange={setEnabled} />
              启用此项
            </label>
          </div>
        </section>
        <section className="space-y-4 rounded-lg border border-border bg-card p-5">
          <h2 className="text-sm font-semibold">交互浮层</h2>
          <div className="flex flex-wrap items-center gap-2">
            <Dialog>
              <DialogTrigger asChild>
                <Button variant="outline">打开 Dialog</Button>
              </DialogTrigger>
              <DialogContent>
                <DialogHeader>
                  <DialogTitle>对话框示例</DialogTitle>
                  <DialogDescription>
                    支持 Escape、焦点锁定和键盘操作。
                  </DialogDescription>
                </DialogHeader>
                <Input aria-label="对话框中的输入" placeholder="可在这里输入" />
              </DialogContent>
            </Dialog>
            <Button variant="outline" onClick={() => setConfirmOpen(true)}>
              打开确认窗口
            </Button>
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button variant="outline">下拉菜单</Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent>
                <DropdownMenuItem>
                  <Copy />
                  复制示例
                </DropdownMenuItem>
                <DropdownMenuItem>
                  <Settings />
                  设置示例
                </DropdownMenuItem>
                <DropdownMenuSeparator />
                <DropdownMenuItem disabled>禁用菜单项</DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
            <Popover>
              <PopoverTrigger asChild>
                <Button variant="outline">Popover</Button>
              </PopoverTrigger>
              <PopoverContent>
                <p className="text-sm">跟随主题的轻量浮层。</p>
              </PopoverContent>
            </Popover>
            <Sheet>
              <SheetTrigger asChild>
                <Button variant="outline">侧边面板</Button>
              </SheetTrigger>
              <SheetContent>
                <SheetHeader>
                  <SheetTitle>Sheet 示例</SheetTitle>
                  <SheetDescription>
                    适用于移动导航或辅助信息。
                  </SheetDescription>
                </SheetHeader>
                <Input aria-label="面板搜索" placeholder="输入示例" />
              </SheetContent>
            </Sheet>
          </div>
          {confirmed && (
            <p role="status" className="text-xs text-success">
              确认交互已完成；未执行任何业务操作。
            </p>
          )}
          <Separator />
          <Tabs defaultValue="tab1">
            <TabsList>
              <TabsTrigger value="tab1">选项卡一</TabsTrigger>
              <TabsTrigger value="tab2">选项卡二</TabsTrigger>
              <TabsTrigger value="tab3" disabled>
                不可用
              </TabsTrigger>
            </TabsList>
            <TabsContent value="tab1" className="text-sm text-muted-foreground">
              键盘方向键可以切换选项卡。
            </TabsContent>
            <TabsContent value="tab2" className="text-sm text-muted-foreground">
              选项卡共享相同的间距和颜色 Token。
            </TabsContent>
          </Tabs>
        </section>
        <section className="grid gap-5 lg:grid-cols-2">
          <div className="overflow-hidden rounded-lg border border-border bg-card">
            <Command>
              <CommandInput placeholder="搜索组件类型…" />
              <CommandList>
                <CommandEmpty>没有匹配项。</CommandEmpty>
                <CommandGroup heading="组件分类">
                  <CommandItem>
                    <MessageSquare />
                    基础控件
                  </CommandItem>
                  <CommandItem>
                    <Bell />
                    交互浮层
                  </CommandItem>
                  <CommandItem>
                    <Sparkles />
                    业务基础组件
                  </CommandItem>
                </CommandGroup>
              </CommandList>
            </Command>
          </div>
          <div className="overflow-hidden rounded-lg border border-border bg-card">
            <DataTable
              caption="组件能力列表"
              columns={[
                { key: "name", header: "组件", cell: (row) => row.name },
                { key: "purpose", header: "用途", cell: (row) => row.purpose },
              ]}
              rows={inventory.filter((row) =>
                row.name.toLowerCase().includes(query.toLowerCase()),
              )}
              rowKey={(row) => row.name}
              pageSize={2}
              pageKey={query}
            />
          </div>
        </section>
        <section className="grid gap-5 lg:grid-cols-3">
          <div className="rounded-lg border border-border bg-card">
            <EmptyState
              title="空状态示例"
              description="页面可以提供一个明确的后续操作。"
              action={
                <Button size="sm" variant="outline">
                  示例操作
                </Button>
              }
            />
          </div>
          <div className="rounded-lg border border-border bg-card">
            <LoadingState
              title="加载状态示例"
              description="不使用虚构数字填补加载中的内容。"
            />
          </div>
          <div className="rounded-lg border border-border bg-card">
            <ErrorState
              message="错误状态示例：连接暂时不可用。"
              onRetry={() => {}}
            />
          </div>
        </section>
        <ConfirmDialog
          open={confirmOpen}
          onOpenChange={setConfirmOpen}
          title="确认交互示例"
          description="这只用于演示确认窗口，不会修改业务数据。"
          confirmLabel="确认示例"
          onConfirm={() => setConfirmed(true)}
        />
      </div>
    </TooltipProvider>
  );
}
