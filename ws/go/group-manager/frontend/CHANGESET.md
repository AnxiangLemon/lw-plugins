# 0.3.2 后续源码与产物清理

- 删除 `frontend/legacy/` 的四个废弃 JS/CSS 文件和 `internal/manager/preview_test.go` 旧人工预览工具。
- 删除 0.2.0～0.3.1 的旧版程序、未分版本的旧程序、旧截图和失效的预览 PID/URL 文件。
- 旧目录的配置、数据库及日志集中移至 `dist/data-archive/`，共 22 个文件，归档前后逐个核对 SHA-256 一致。
- `dist/v0.3.2/`、当前运行数据、最新截图保留原位；通用组件库、组件示例、有效测试与构建输入继续保留。
- 同步更新两份 README、ESLint 忽略项和本记录，清除失效的源码引用。

清理后重新执行前端构建（含 TypeScript）、ESLint、14 项前端测试，以及 `go test ./...`、`go vet ./...`，均通过。生产静态资源摘要保持一致，本次无需替换正在运行的 0.3.2 程序。

---

# 0.3.2 字号、密度与配色调整

本次没有新增或升级依赖。保持业务 API、数据结构和交互流程，正文与表格统一为 15px，辅助文字为 13px；模块间距 16px，桌面页边距 24px。提亮深色卡片，以蓝青色点缀图表和图标，白色主按钮与灰色选中态继续保留。

修改文件（以下均相对 `ws/go/group-manager/`）：

- `frontend/src/styles/globals.css`：明暗主题与字号 Token、小屏基础宽度。
- `frontend/src/app/AppLayout.tsx`：导航字号、页面间距、图标点缀。
- `frontend/src/components/shared/{data-table,empty-state,loading-state,error-state,page-header}.tsx`：紧凑行距、标题、加载与空状态；新增 `EmptyState compact`。
- `frontend/src/components/ui/{button,badge,tabs,dialog,alert-dialog}.tsx`：控件字号、浮层间距。
- `frontend/src/pages/{DashboardPage,TrendChart,GroupsPage,InvitersPage,HistoryPage,OperationsPage,SettingsPage,page-controls}.tsx`：卡片、表格、图表与筛选布局。
- `frontend/src/features/groups/{group-dialogs,action-editor,group-tracking,operation-result,group-members}.tsx`：群管理弹窗与成员列表的字号、间距。
- `main.go`、`frontend/package.json`、`frontend/package-lock.json`：版本同步为 0.3.2。
- `README.md`、`frontend/README.md`、本文件：规范与验收记录。
- `internal/manager/web/`：重新生成内嵌页面及静态资源。

验证：构建及 TypeScript 检查通过，ESLint 通过，原有 14 项前端测试通过，`go test ./...` 通过。浏览器使用当前真实接口数据检查六个页面、群管理弹窗、深浅主题、64px 收缩导航及 390px / 320px 小屏；修复 320px 宽度下纵向滚动条导致的横向溢出，控制台无警告或错误。当前数据为空的列表只验收真实空态，未制造成员或操作数据。

可执行文件位于 `dist/v0.3.2/`；实际截图位于 `dist/ui-0.3.2/`。

---

# 0.3.0 UI 改造文件与验收记录

本次范围为 `ws/go/group-manager`，沿用六个 hash 页面及现有业务 API。相邻 Desktop、Rust SDK 与 Go SDK 没有因本次 UI 改造发生代码或依赖升级。

## 修改文件

- `ws/go/group-manager/main.go`：群管家版本 0.3.0。
- `ws/go/group-manager/internal/manager/http.go`：只适配静态 HTML 的每请求样式 nonce，业务 API 及同源保护不变。
- `ws/go/group-manager/README.md`：构建、运行和新版前端维护说明。
- `.github/workflows/go.yml`：群管家矩阵增加前端 CI，SDK 流程保留。
- `ws/go/group-manager/internal/manager/web/index.html`：Vite 构建入口。

## 新增 Go 测试

- `ws/go/group-manager/internal/manager/frontend_assets_test.go`：独立 nonce、静态资源和同源检查。

## 新增前端源文件

下面路径以 `ws/go/group-manager/frontend/` 为前缀；node_modules、构建缓存和截图不属于源码。

- `.gitignore`
- `components.json`
- `eslint.config.mjs`
- `go.mod`
- `index.html`
- `package-lock.json`
- `package.json`
- `public/favicon.svg`
- `public/theme-init.js`
- `README.md`
- `scripts/embed.mjs`
- `src/app/App.tsx`
- `src/app/AppLayout.tsx`
- `src/app/data-context.tsx`
- `src/app/theme-provider.tsx`
- `src/components/examples/ComponentExamples.tsx`
- `src/components/shared/confirm-dialog.tsx`
- `src/components/shared/data-table.tsx`
- `src/components/shared/empty-state.tsx`
- `src/components/shared/error-state.tsx`
- `src/components/shared/loading-state.tsx`
- `src/components/shared/page-header.tsx`
- `src/components/shared/search-input.tsx`
- `src/components/ui/alert-dialog.tsx`
- `src/components/ui/avatar.tsx`
- `src/components/ui/badge.tsx`
- `src/components/ui/button.tsx`
- `src/components/ui/checkbox.tsx`
- `src/components/ui/command.tsx`
- `src/components/ui/dialog.tsx`
- `src/components/ui/dropdown-menu.tsx`
- `src/components/ui/input.tsx`
- `src/components/ui/popover.tsx`
- `src/components/ui/select.tsx`
- `src/components/ui/separator.tsx`
- `src/components/ui/sheet.tsx`
- `src/components/ui/switch.tsx`
- `src/components/ui/tabs.tsx`
- `src/components/ui/textarea.tsx`
- `src/components/ui/tooltip.tsx`
- `src/features/groups/action-editor.tsx`
- `src/features/groups/group-dialogs.tsx`
- `src/features/groups/group-members.tsx`
- `src/features/groups/group-tracking.tsx`
- `src/features/groups/operation-result.tsx`
- `src/lib/api.ts`
- `src/lib/format.ts`
- `src/lib/types.ts`
- `src/lib/utils.ts`
- `src/main.tsx`
- `src/pages/DashboardPage.tsx`
- `src/pages/GroupsPage.tsx`
- `src/pages/HistoryPage.tsx`
- `src/pages/InvitersPage.tsx`
- `src/pages/OperationsPage.tsx`
- `src/pages/page-controls.tsx`
- `src/pages/SettingsPage.tsx`
- `src/pages/TrendChart.tsx`
- `src/styles/globals.css`
- `tests/components.test.tsx`
- `tests/group-dialogs.test.tsx`
- `tests/pages.test.tsx`
- `tests/setup.ts`
- `tsconfig.json`
- `vite.config.ts`
- `CHANGESET.md`：本文件。

## 迁移参考文件

0.3.0 迁移时曾将旧的 `app.js`、`ui.js`、`style.css`、`ui.css` 暂存用于核对；这些迁移备份已在 0.3.2 清理。当前静态资源统一来自 Vite 构建，`frontend/public/` 保留图标与主题初始化脚本作为构建输入。

## 生成资源

`npm run build` 生成 `frontend/dist/` 并同步到 `internal/manager/web/`：入口 HTML、favicon.svg、theme-init.js、主脚本、主题 CSS、按需加载的 ECharts 脚本。assets 文件名包含内容摘要，后续构建可能变化，不应手工修改。

## 已执行验证

- `npm run typecheck`、`npm run lint`、`npm run build` 通过。
- Vitest 3 个文件、14 项测试通过：组件键盘与焦点、分页筛选重置、主题持久化/System、配置草稿与群配置合并、默认通知关闭、原回执查询、公告读取失败保护、稳定操作编号等。
- `go test -race ./...` 和 `go vet ./...` 通过。
- Windows 可执行文件启动显示 0.3.0；Linux amd64 交叉构建通过。
- 浏览器使用既有本机 API 的真实数据逐页检查；没有向产品植入演示群、虚构增长或虚构邀请数据。
- 1440px、1024px、390px 均无页面级横向溢出；窄屏表格在自身容器内滚动，Sidebar 使用 Sheet。
- 验证深浅主题、刷新后保留、64px Sidebar 收缩、账号搜索、群筛选、搜索空状态、群设置确认与取消、未保存偏好跨页保留、ECharts Tooltip。
- 独立空数据 Go 实例验证设置保存及校验、Select/Dialog/Command、最终 ECharts 资源加载和严格 CSP；真实群配置未用于写入测试。
- 最终生产构建浏览器控制台无 error/warn；Go 实例的动态样式带 nonce，未增加 unsafe-inline。

## 仍需真实微信业务验证的范围

公告发布、踢人、管理员变更、转让群主、退出群等真实微信写操作未在客户群执行。本轮验证的是页面、请求契约、确认与幂等保护，不将单元测试等同于真实微信执行成功。入群审批继续按用户要求暂缓。

## 历史产物

0.3.0 的截图与可执行文件已清理，当前产物见本文开头的 0.3.2 记录。旧版本运行数据归档于 `dist/data-archive/`。
