import {
  Component,
  lazy,
  Suspense,
  useEffect,
  useState,
  type ErrorInfo,
  type ReactNode,
} from "react";
import { useAppData } from "./data-context";
import { AppLayout } from "./AppLayout";
import { DashboardPage } from "@/pages/DashboardPage";
import { GroupsPage } from "@/pages/GroupsPage";
import { InvitersPage } from "@/pages/InvitersPage";
import { HistoryPage } from "@/pages/HistoryPage";
import { OperationsPage } from "@/pages/OperationsPage";
import { SettingsPage } from "@/pages/SettingsPage";
import { LoadingState } from "@/components/shared/loading-state";
import { ErrorState } from "@/components/shared/error-state";
import type { Page } from "@/lib/types";

// 示例只在开发构建中存在，不添加生产业务路由，也不伪造任何群数据。
const ComponentExamples = import.meta.env.DEV
  ? lazy(() => import("@/components/examples/ComponentExamples"))
  : null;
class PageBoundary extends Component<
  { children: ReactNode },
  { failed: boolean }
> {
  state = { failed: false };
  static getDerivedStateFromError() {
    return { failed: true };
  }
  componentDidCatch(error: Error, info: ErrorInfo) {
    console.error("页面渲染失败", error, info.componentStack);
  }
  render() {
    return this.state.failed ? (
      <ErrorState
        message="页面暂时无法显示，请刷新重试。"
        onRetry={() => location.reload()}
      />
    ) : (
      this.props.children
    );
  }
}
export function App() {
  const { page } = useAppData();
  const [visited, setVisited] = useState<Set<Page>>(() => new Set([page]));
  useEffect(() => {
    setVisited((previous) =>
      previous.has(page) ? previous : new Set([...previous, page]),
    );
  }, [page]);
  const pages = {
    dashboard: DashboardPage,
    groups: GroupsPage,
    inviters: InvitersPage,
    history: HistoryPage,
    operations: OperationsPage,
    settings: SettingsPage,
  };
  return (
    <AppLayout>
      <PageBoundary>
        {ComponentExamples &&
        new URLSearchParams(location.search).has("components") ? (
          <Suspense fallback={<LoadingState />}>
            <ComponentExamples />
          </Suspense>
        ) : (
          Object.entries(pages).map(
            ([key, PageComponent]) =>
              (visited.has(key as Page) || key === page) && (
                <div key={key} hidden={key !== page}>
                  <PageComponent />
                </div>
              ),
          )
        )}
      </PageBoundary>
    </AppLayout>
  );
}
