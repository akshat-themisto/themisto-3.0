"use client";

import { Area, AreaChart, CartesianGrid, ResponsiveContainer, Tooltip, XAxis, YAxis } from "recharts";
import { formatDateTime } from "@/lib/format";

type ActionTrendChartProps = {
  data: Array<{ day: string; critical_actions: number | string; total_actions: number | string }>;
};

export function ActionTrendChart({ data }: ActionTrendChartProps) {
  const chartData = data.map((item) => ({
    ...item,
    dayLabel: new Date(item.day).toLocaleDateString(undefined, { month: "short", day: "numeric" }),
    total: Number(item.total_actions),
    critical: Number(item.critical_actions)
  }));

  return (
    <div className="overview-chart">
      <ResponsiveContainer width="100%" height="100%">
        <AreaChart data={chartData} margin={{ top: 16, right: 16, left: -12, bottom: 0 }}>
          <defs>
            <linearGradient id="trendTotal" x1="0" y1="0" x2="0" y2="1">
              <stop offset="5%" stopColor="#c97c85" stopOpacity={0.36} />
              <stop offset="95%" stopColor="#c97c85" stopOpacity={0.02} />
            </linearGradient>
            <linearGradient id="trendCritical" x1="0" y1="0" x2="0" y2="1">
              <stop offset="5%" stopColor="#f4a6aa" stopOpacity={0.22} />
              <stop offset="95%" stopColor="#f4a6aa" stopOpacity={0.02} />
            </linearGradient>
          </defs>
          <CartesianGrid stroke="rgba(255,255,255,0.06)" vertical={false} />
          <XAxis dataKey="dayLabel" stroke="#8f9d93" tickLine={false} axisLine={false} />
          <YAxis stroke="#8f9d93" tickLine={false} axisLine={false} />
          <Tooltip
            contentStyle={{ background: "#171d19", border: "1px solid rgba(255,255,255,0.08)", borderRadius: 16 }}
            labelFormatter={(_, payload) => (payload?.[0] ? formatDateTime(payload[0].payload.day) : "")}
          />
          <Area type="monotone" dataKey="total" stroke="#c97c85" fill="url(#trendTotal)" strokeWidth={3} />
          <Area type="monotone" dataKey="critical" stroke="#f4a6aa" fill="url(#trendCritical)" strokeWidth={2.5} />
        </AreaChart>
      </ResponsiveContainer>
    </div>
  );
}

