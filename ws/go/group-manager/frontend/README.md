# 群管家前端维护说明

群管家前端使用 React、TypeScript、Tailwind CSS 和 Vite。开发与构建需要 Node.js 22.12+，依赖版本以 `package-lock.json` 为准。运行时由 Go 程序提供内嵌页面和同源 `/api/*`。

## 开发与验证

在 `frontend/` 中运行以下命令。

```sh
npm ci
npm run lint
npm test
npm run build
```

`npm run dev` 启动 `http://127.0.0.1:5178`，将 `/api` 代理到 `http://127.0.0.1:8766`。真实 API 与 CSP 可通过 [Go 内嵌服务](../README.md#构建与运行)验证。

`npm run build` 执行 TypeScript 检查和 Vite 构建，然后用 `scripts/embed.mjs` 同步资源至 `../internal/manager/web/`。将更新后的内嵌资源与前端源码一并提交。回到群管家目录后运行 `go test -race ./...` 和 `go vet ./...`。

浏览器回归可检查深色、浅色、跟随系统、主题持久化、六个页面、窄屏布局及弹层焦点。组件与接口测试使用 `tests/` 中的本地 fixture。

## 目录与页面

```text
frontend/
  src/
    app/                 # AppLayout、主题、共享数据与页面生命周期
    components/ui/       # 基础和交互组件，不访问业务 API
    components/shared/   # DataTable、PageHeader、通用状态与确认
    components/examples/ # 仅开发环境的组件示例
    features/groups/     # 群资料、成员、提醒、操作确认与结果
    pages/               # 六个业务页面，趋势图按需加载
    lib/                 # API、严格字段类型、格式化工具
    styles/globals.css   # 颜色 Token、主题、全局基础样式
  tests/                 # 隔离的组件/契约测试
  public/                # 同源主题初始化脚本、图标等静态资源
  scripts/embed.mjs      # 将构建产物同步给 Go embed
  dist/                  # Vite 生成目录
```

页面使用同一 `AppLayout`。首次访问后保留页面挂载，切换页面不丢弃搜索条件或未保存草稿；页面隐藏后停止专属列表轮询。页面导航保留原 hash，不增加 `#/` 前缀，浏览器后退/前进仍可使用。

| Hash | 页面 | 主要职责 |
| --- | --- | --- |
| `#dashboard` | 数据概览 | 四项统计、真实成员趋势、邀请贡献、最近动态、使用引导 |
| `#groups` | 群聊管理 | 群列表、搜索、记录开关、资料/成员/提醒管理 |
| `#inviters` | 邀请人统计 | 按账号/群/时间统计邀请贡献和未知来源 |
| `#history` | 成员动态 | 搜索筛选、已加载数据分页、CSV、原通知回执 |
| `#operations` | 操作记录 | 状态筛选、操作详情、查询原操作结果 |
| `#settings` | 偏好设置 | 三种主题、数值设置、连接与本地数据说明 |

`app/data-context.tsx` 保存服务端状态。配置页面将本地草稿合并到最新完整 `Config`，轮询期间保留正在编辑的草稿。账号与群的身份由 `account_id` 和 `room_id` 共同确定。

`DataTable` 按已加载数组分页。成员动态接口最多返回 200 条，操作接口最多返回 100 条。邀请贡献使用后端按时间段聚合的数据。

## 依赖

| 依赖 | 用途 |
| --- | --- |
| `react`、`react-dom` | 页面渲染与状态 |
| `radix-ui` | Select、Dialog、Popover、Tooltip、Tabs 等交互原语 |
| `cmdk` | 账号搜索和快速导航 |
| `class-variance-authority`、`clsx`、`tailwind-merge` | 组件变体与 Tailwind 类名合并 |
| `lucide-react` | 统一图标 |
| `echarts` | 按需注册的 Canvas 折线图和 Hover Tooltip |
| `get-nonce` | 将入口 HTML 的 CSP nonce 传给动态滚动锁样式 |
| TypeScript、Vite、Tailwind、ESLint、Vitest、Testing Library、jsdom | 开发、构建与检查工具 |

## 主题与样式

`src/styles/globals.css` 是颜色唯一来源。`:root` 定义浅色，`.dark` 定义深色，Tailwind v4 的 `@theme inline` 将变量映射为 `bg-background`、`text-muted-foreground` 等语义类名。默认入口和 `ThemeProvider` 均采用深色，页面背景不使用纯黑；页面、选中态和主按钮保持中性灰白，卡片采用更明亮的表面；`highlight` 蓝青色点缀导航图标与趋势，成功、警告和失败保留独立语义色。

| Token | 深色 | 浅色 | 用途 |
| --- | --- | --- | --- |
| `--background` | `#1c1c1f` | `#fafafa` | 页面背景 |
| `--card` | `#28282d` | `#ffffff` | 内容表面 |
| `--popover` | `#303036` | `#ffffff` | 菜单、弹窗 |
| `--foreground` | `#f4f4f5` | `#18181b` | 主要文字 |
| `--muted-foreground` | `#b0b0bb` | `#71717a` | 次要说明 |
| `--brand` | `#f4f4f5` | `#18181b` | 品牌标识 |
| `--primary` | `#f4f4f5` | `#18181b` | 可操作重点；浅色下加深以保证可读性 |
| `--border` | `#3d3d45` | `#e4e4e7` | 必要的内容分隔 |
| `--highlight` | `#42c7eb` | `#087da1` | 导航、图标与图表点缀 |
| `--ring` | `#42c7eb` | `#087da1` | 键盘焦点 |
| `--success` | `#4ab787` | `#23835a` | 正常与成功 |
| `--warning` | `#d8bc82` | `#976521` | 待确认与提醒 |
| `--destructive` | `#ee9696` | `#b23b3b` | 失败、破坏性操作 |

其他语义色包含 `secondary`、`muted`、`accent`、`input`、`sidebar`、`overlay` 和 `chart-*`，均在同一文件中成对定义。弹层只用 `shadow-popover` 轻阴影，不使用大面积渐变。表格统一使用 15px 正文、紧凑行内间距；`EmptyState compact` 用于概览与列表空态，减少无数据时的留白。

| 规范 | 值 |
| --- | --- |
| 字体 | Inter 优先，回退系统 UI 与中文字体；无在线字体请求 |
| 正文 / 辅助 / 标题 | 15px / 13px / 24px |
| 字重 | 400 正文、500 控件、600 标题和关键数字 |
| 数字 | `tabular-nums` 保持统计和表格对齐 |
| 基础间距 | 4px 网格；控件间 8–12px、模块间 16px |
| 页面左右边距 | 桌面 24px，小屏 16–24px |
| Sidebar | 展开 224px，收缩 64px，导航行高 38px |
| 圆角 | 基准 8px；小控件 4–6px，弹窗最多 12px |
| 边框 | 1px 语义边框，避免逐层叠加 |
| 动画 | 短时淡入，尊重 `prefers-reduced-motion` |

`app/theme-provider.tsx` 是唯一主题状态源，提供 `theme`、`resolvedTheme`、`setTheme`。`theme` 为 `dark` / `light` / `system`；选择保存至 `lw-group-manager-theme`，System 监听操作系统变化，并同步其他标签页的选择。组件使用语义类，图表从 CSS Variables 读取颜色，因此浮层和图表随主题更新。

## 组件库与使用示例

基础组件：Button、Input、Textarea、Select、Checkbox、Switch、Badge、Avatar、Separator。

交互组件：Dialog、AlertDialog、DropdownMenu、Popover、Tooltip、Tabs、Sheet、Command。

通用业务组合：DataTable、EmptyState、LoadingState、ErrorState、ConfirmDialog、SearchInput、PageHeader。Props 使用 TypeScript 定义；异步确认交由调用方，通用组件不会自行请求业务接口。

```tsx
import { Button } from "@/components/ui/button";
import { PageHeader } from "@/components/shared/page-header";
import { DataTable } from "@/components/shared/data-table";

type Row = { id: string; title: string };

export function ListExample({ rows, loading, refresh }: {
  rows: Row[];
  loading: boolean;
  refresh: () => void;
}) {
  return <>
    <PageHeader title="列表" description="由调用方传入真实数据。"
      actions={<Button variant="outline" loading={loading} onClick={refresh}>刷新</Button>} />
    <DataTable rows={rows} rowKey={(row) => row.id} pageSize={15}
      columns={[{ key: "title", header: "名称", cell: (row) => row.title }]} />
  </>;
}
```

运行 `npm run dev` 后打开 `http://127.0.0.1:5178/?components` 查看完整组件示例。该入口只在开发构建存在，展示组件名称与交互，不伪造群聊或统计数据，也不增加生产业务路由。

## API 与浏览器约定

`lib/api.ts` 集中发送 JSON 请求并设置 `X-Plugin-Request: 1`。`lib/types.ts` 定义请求字段，Go 校验 Host、Origin 和字段白名单。写操作返回 `unknown` 时，页面查询原回执。

Go 为每次入口 HTML 响应生成独立的样式 nonce，Radix 通过 `get-nonce` 使用它添加滚动锁样式。生产脚本仅从同源加载。目标浏览器为支持 ES2022 和 Tailwind v4 的现代 Chromium 或 WebView2。

历史迁移和验收记录见 [CHANGESET.md](CHANGESET.md)。
