import { useEffect, useRef } from "react";
import * as echarts from "echarts";
import type { EChartsOption } from "echarts";

export function OverviewChart({
  option,
  testId,
  height = 280,
}: {
  option: EChartsOption;
  testId?: string;
  height?: number;
}) {
  const containerRef = useRef<HTMLDivElement | null>(null);

  useEffect(() => {
    if (!containerRef.current) {
      return;
    }

    const chart = echarts.getInstanceByDom(containerRef.current) ?? echarts.init(containerRef.current);
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
