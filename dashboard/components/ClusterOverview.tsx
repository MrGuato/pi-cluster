import MetricCard from "./MetricCard";
import type { ClusterOverviewData } from "@/lib/types";

interface ClusterOverviewProps {
  data: ClusterOverviewData | null;
}

export default function ClusterOverview({ data }: ClusterOverviewProps) {
  const loading = data === null;

  return (
    <section className="mb-8">
      <h2 className="text-xs font-semibold text-slate-500 uppercase tracking-widest mb-4">
        Cluster Overview
      </h2>
      <div className="grid grid-cols-2 lg:grid-cols-4 gap-3">
        <MetricCard
          label="CPU Usage"
          value={loading ? "—" : `${data.cpuPercent.toFixed(1)}%`}
          icon={<span>⚡</span>}
        />
        <MetricCard
          label="Memory Usage"
          value={loading ? "—" : `${data.memoryPercent.toFixed(1)}%`}
          icon={<span>🧠</span>}
        />
        <MetricCard
          label="Running Pods"
          value={loading ? "—" : `${data.pods}`}
          icon={<span>📦</span>}
        />
        <MetricCard
          label="Services"
          value={loading ? "—" : `${data.services}`}
          icon={<span>🔗</span>}
        />
      </div>
    </section>
  );
}

