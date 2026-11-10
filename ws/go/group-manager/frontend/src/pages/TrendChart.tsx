import { useEffect, useRef } from "react";
import { init, use as registerECharts, type ECharts } from "echarts/core";
import { LineChart } from "echarts/charts";
import { GridComponent, TooltipComponent } from "echarts/components";
import { CanvasRenderer } from "echarts/renderers";
import type { TrendPoint } from "@/lib/types";
import { useTheme } from "@/app/theme-provider";

// 按需注册，避免把地图、3D及所有图表类型打进本地插件。
registerECharts([LineChart, GridComponent, TooltipComponent, CanvasRenderer]);
export default function TrendChart({ points }: { points: TrendPoint[] }) {
  const element = useRef<HTMLDivElement>(null);
  const instance = useRef<ECharts | null>(null);
  const { resolvedTheme } = useTheme();
  useEffect(() => {
    if (!element.current) return;
    const chart = init(element.current, undefined, { renderer: "canvas" });
    instance.current = chart;
    const observer = new ResizeObserver(() => chart.resize());
    observer.observe(element.current);
    return () => {
      observer.disconnect();
      chart.dispose();
      instance.current = null;
    };
  }, []);
  useEffect(() => {
    const chart = instance.current;
    if (!chart) return;
    const styles = getComputedStyle(document.documentElement);
    const color = (token: string) => styles.getPropertyValue(token).trim();
    chart.setOption({
      animationDuration: 200,
      textStyle: {
        fontFamily: styles.getPropertyValue("--font-sans"),
        fontSize: 12,
      },
      grid: { left: 44, right: 22, top: 14, bottom: 30 },
      // richText Tooltip 由 Canvas 绘制，遵守 Go 的 CSP，也不会把群名当 HTML。
      tooltip: {
        trigger: "axis",
        renderMode: "richText",
        confine: true,
        backgroundColor: color("--popover"),
        borderColor: color("--border"),
        textStyle: { color: color("--foreground"), fontSize: 13 },
        axisPointer: {
          type: "line",
          lineStyle: {
            color: color("--muted-foreground"),
            type: "dashed",
            opacity: 0.4,
          },
        },
      },
      xAxis: {
        type: "category",
        boundaryGap: false,
        data: points.map((point) => point.day.slice(5).replace("-", "/")),
        axisLine: { show: false },
        axisTick: { show: false },
        axisLabel: {
          color: color("--muted-foreground"),
          margin: 12,
          hideOverlap: true,
        },
      },
      yAxis: {
        type: "value",
        min: 0,
        minInterval: 1,
        axisLabel: { color: color("--muted-foreground") },
        splitLine: {
          lineStyle: {
            color: color("--border"),
            type: "dashed",
            opacity: 0.65,
          },
        },
      },
      series: [
        {
          name: "新增成员",
          type: "line",
          data: points.map((point) => point.joined),
          symbol: "circle",
          symbolSize: 6,
          showSymbol: points.length === 1,
          smooth: false,
          lineStyle: { color: color("--chart-join"), width: 2.5 },
          itemStyle: { color: color("--chart-join") },
          areaStyle: { color: color("--chart-join"), opacity: 0.08 },
        },
        {
          name: "离群成员",
          type: "line",
          data: points.map((point) => point.left),
          symbol: "circle",
          symbolSize: 5,
          showSymbol: points.length === 1,
          smooth: false,
          lineStyle: {
            color: color("--chart-left"),
            width: 1.5,
            type: "dashed",
          },
          itemStyle: { color: color("--chart-left") },
        },
      ],
    });
  }, [points, resolvedTheme]);
  // 高度由仪表盘容器控制，ResizeObserver 同时适配紧凑空态与正常趋势图。
  return (
    <div
      ref={element}
      role="img"
      aria-label={`成员变化趋势，${points.length}天，新增${points.reduce((sum, p) => sum + p.joined, 0)}人，离群${points.reduce((sum, p) => sum + p.left, 0)}人`}
      className="h-full min-h-0 w-full"
    />
  );
}
