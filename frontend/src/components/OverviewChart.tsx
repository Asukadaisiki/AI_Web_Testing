import { useEffect, useRef } from "react";
import { BarChart, LineChart } from "echarts/charts";
import { GridComponent, LegendComponent, TooltipComponent } from "echarts/components";
import { type EChartsCoreOption, getInstanceByDom, init, use } from "echarts/core";
import { CanvasRenderer } from "echarts/renderers";

use([BarChart, LineChart, GridComponent, LegendComponent, TooltipComponent, CanvasRenderer]);

export function OverviewChart({
  option,
  testId,
  height = 280,
}: {
  option: EChartsCoreOption;
  testId?: string;
  height?: number;
}) {
  const containerRef = useRef<HTMLDivElement | null>(null);

  useEffect(() => {
    if (!containerRef.current) {
      return;
    }

    const chart = getInstanceByDom(containerRef.current) ?? init(containerRef.current);
    chart.setOption(option, true);

    const handleResize = () => {
      chart.resize();
    };

    window.addEventListener("resize", handleResize);
    return () => {
      window.removeEventListener("resize", handleResize);
      chart.dispose();
    };
  }, [option]);

  return <div ref={containerRef} data-testid={testId} className="overview-chart" style={{ height }} />;
}
