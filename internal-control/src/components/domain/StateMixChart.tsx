"use client";

import { Cell, Pie, PieChart, ResponsiveContainer, Tooltip } from "recharts";

type StateMixChartProps = {
  data: Array<{ status: string; count: number | string }>;
};

const colors: Record<string, string> = {
  active: "#7cc79c",
  suspended: "#d0bf78",
  terminated: "#a94a55"
};

export function StateMixChart({ data }: StateMixChartProps) {
  const chartData = data.map((item) => ({ ...item, count: Number(item.count) }));

  return (
    <div className="overview-chart" style={{ height: 300 }}>
      <ResponsiveContainer width="100%" height="100%">
        <PieChart>
          <Pie data={chartData} dataKey="count" nameKey="status" innerRadius={70} outerRadius={110} paddingAngle={4}>
            {chartData.map((entry) => (
              <Cell key={entry.status} fill={colors[entry.status] ?? "#8f9d93"} />
            ))}
          </Pie>
          <Tooltip contentStyle={{ background: "#171d19", border: "1px solid rgba(255,255,255,0.08)", borderRadius: 16 }} />
        </PieChart>
      </ResponsiveContainer>
    </div>
  );
}

