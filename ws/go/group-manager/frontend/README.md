# 群管家前端维护说明

本目录是 Go 群管家 0.3.2 的前端工程。用户运行一个 Go 可执行文件即可使用面板；Node.js 仅用于开发、测试和构建。业务数据来自同源 `/api/*`，本工程不建立新的微信连接，不读取宿主 token。

## A. 技术栈审计

| 项目 | 改造前的群管家 0.2.1 | 当前群管家 0.3.2 |
| --- | --- | --- |
| React / React DOM | 未安装，无 `package.json` | 19.3.1 / 19.3.1 |
| TypeScript | 原生 JavaScript | 5.9.x |
| Tailwind CSS | 未安装，手写 CSS | 4.3.5，使用 `@tailwindcss/vite` |
| 构建工具 | 无；Go 直接 embed 静态源码 | Vite 8.3.7 + React 插件 6.3.0 |
| 交互组件 | 手写 `ui.js` 菜单、弹窗、校验 | 本地组件源码 + Radix 1.8.1 + cmdk 1.1.1 |
| shadcn/ui | 不存在 | `components.json` 与 shadcn/ui 风格的本地组件；不需要运行时 shadcn 包 |
| 图标与图表 | 手绘 SVG + canvas | lucide-react 1.71.0 + ECharts 6.1.0 |
| 状态 | `app.js` 模块变量 + 定时 fetch | React Context + 页面草稿，保持原接口 |
| 检查 | Go 测试；JS 语法检查 | TypeScript、ESLint、Vitest、Go 测试 |

相邻 `lw-desktop/frontend` 是独立的 Vue 3.5.44 / Vue Router 4 / Vite 8.3.5 工程，本轮没有迁移或升级 Desktop 的依赖。群管家原本没有 React、Tailwind 或组件库，因此这里是新增独立前端工程，并非替换或升级既有库。版本以 `package.json` 和 `package-lock.json` 为准，使用 `npm ci` 安装；不要为添加一个组件运行整库初始化覆盖源码。

## B. UI 架构与目录

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

`app/data-context.tsx` 保存已读取的服务端状态，轮询不回填正在编辑的表单。配置保存仍使用完整 `Config`，页面仅将草稿合并到最新配置；账号与群的身份始终由 `account_id` + `room_id` 共同确定。无需 Redux、Zustand 或额外路由库。

## C. 保留的行为与可复用部分

- Go 的目录发现、名单完整性校验、快照差集、邀请来源、持久化、通知调度及幂等操作保持原有实现。
- 六个页面入口、同源请求约定、账号隔离、完整配置结构和结果状态继续复用。
- 首次记录不产生全员入群事件；入群与离群默认仅记录，发消息由两个独立开关控制。
- 群编辑、公告预读、本人昵称、通讯录、二维码、成员管理、群主转让、退出及本人链接入群保留。
- 不确定的写操作只查询原回执，不换新 ID 自动重试；成员动态仍明确区分发现时间与离线补记。

原 DOM 组件依赖全局变量和手动重建节点，已由 React 组件替代；迁移保留业务语义，旧源码备份已清理。

## D. 已重构的 UI 边界

Sidebar、PageHeader、Table、Dialog、Button、菜单、开关、表单和空/错误状态均改为类型化组件。`components/ui` 负责视觉和无障碍交互，`components/shared` 负责通用组合，`features` 与 `pages` 才能读取业务 API。业务组件不要复制通用控件，也不要直接写十六进制颜色。

表格使用轻量泛型 `DataTable`，按已加载数组分页；成员动态接口最多返回 200 条、操作接口最多返回 100 条，界面不会把此数量当作完整历史总数。邀请贡献继续调用后端完整时间段聚合，不从最近日志拼出统计。

## E. 新增依赖及用途

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

没有加入整套业务 UI 框架、日期库、表单库或图表 React 包装器。图表读取同一主题 Token，颜色切换不维护第二套色板。

## Design System

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

## F. 兼容性与迁移风险

- **嵌入资源同步：** 修改 `src/` 后必须运行 `npm run build` 再构建 Go。直接修改 `internal/manager/web/assets/` 会被下一次前端构建覆盖。
- **浏览器与构建环境：** Node.js 至少 22.12，浏览器使用现代 Chromium/WebView2；Tailwind v4 和 ES2022 构建以现代浏览器为目标。
- **CSP：** 只对入口 HTML 添加每次请求独立的样式 nonce。`get-nonce` 供 Radix 动态样式使用，脚本仍限同源，不增加 `unsafe-inline`；API 的原 Host/Origin/请求头校验保留。
- **API 边界：** `lib/api.ts` 沿用 JSON + `X-Plugin-Request: 1`。请求只发送白名单字段，Go 拒绝未知字段。所有写入不自动重试。
- **状态与异步：** 账号切换隔离查询结果；列表轮询跳过在途请求，过期响应不覆盖新筛选。表单草稿独立于轮询状态。
- **业务语义：** `unknown` 是待确认，不是失败。邀请来源为空保持未知，首次名单不计新增；未添加入群审批。
- **工程边界：** Go 的配置、数据模型和业务接口未变，数据库继续使用已有迁移。Desktop Vue 工程保持独立。

读取接口包括 `/api/status`、`/api/accounts`、`/api/rooms`、`/api/dashboard`、`/api/events`、`/api/members`、`/api/operations` 和 `/api/events.csv`。配置、发现、重查、成员/公告读取以及操作/回执沿用现有 POST 端点，具体字段集中在 `lib/types.ts`，不在控件内散落请求逻辑。

## G. 分阶段实施与验证

1. **审计与边界确认：** 核对原技术栈、六个页面、全部群操作和 Go 的 CSP/同源约定。
2. **工程与 Design System：** 新建独立 Vite 工程、语义 Token、深浅主题和 ThemeProvider。
3. **公共组件：** 基础组件、交互组件和通用状态分层实现，检查类型和构建，再接入页面。
4. **布局与 Dashboard：** 统一 AppLayout、可收缩导航、账号搜索、ECharts 和真实空状态。
5. **业务页面迁移：** 逐页迁移并核对筛选、分页、表单、结果查询及配置保存语义。
6. **质量验收：** 类型、ESLint、单元测试、Go 测试、实际浏览器多主题和多分辨率检查；最后构建内嵌资源。

在 `frontend/` 中运行：

```sh
npm ci
npm run typecheck
npm run lint
npm test
npm run build
```

`npm run build` 先执行类型检查，再由 Vite 生成 `dist/`，最后由 `scripts/embed.mjs` 同步至 `../internal/manager/web/`。脚本仅清理自己管理的 `assets/`，不会读取或复制 endpoint、配置和数据库。随后回到群管家目录执行 `go test ./...`、`go vet ./...` 和 `go build`。

开发启动 `npm run dev`，`/api` 代理到 `http://127.0.0.1:8766`。代理仅在开发阶段调整同源请求头，生产环境由 Go 同源服务。`npm run preview` 只用于查看已构建静态页面；真实 API 和 CSP 验收应使用 Go 内嵌服务。

本次工程检查已通过 TypeScript、ESLint 和 14 项前端单元测试。测试输入只存在于 `tests/`，用于验证默认不开启消息、保留最新群配置、原回执查询、跨账号隔离、焦点和主题等契约；不注入生产数据，不以单元测试代替真实微信操作验证。旧人工合成预览工具已清理，后续统一通过 Vite 或 Go 内嵌页面检查布局。

浏览器回归使用已有本机服务的实际数据，或明确的独立空数据实例。验收应记录深/浅/System、刷新后主题、六个 hash、窄屏与桌面、弹层焦点和控制台错误；对会修改微信群的操作，先核对对应测试条件，再记录实际执行结果。
