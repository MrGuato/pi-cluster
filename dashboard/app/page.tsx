"use client";

import { useClusterData } from "@/lib/hooks";
import Header from "@/components/Header";
import InfrastructureStack from "@/components/InfrastructureStack";
import ClusterOverview from "@/components/ClusterOverview";
import ClusterNodes from "@/components/ClusterNodes";

export default function DashboardPage() {
  const { data: clusterData, error, isLoading } = useClusterData();

  // Derive API status from the cluster data fetch state
  const apiStatus = error
    ? "degraded"
    : isLoading
      ? "unknown"
      : clusterData
        ? "healthy"
        : "unknown";

  const apiDetail = error
    ? "Unable to reach cluster metrics"
    : isLoading
      ? "Loading..."
      : "All systems operational";

  return (
    <main className="max-w-6xl mx-auto px-4 py-8">
      <Header apiStatus={apiStatus} apiDetail={apiDetail} />

      <InfrastructureStack />

      <ClusterOverview data={clusterData?.overview ?? null} />

      <ClusterNodes nodes={clusterData?.nodes ?? null} />

      <footer className="mt-12 pt-4 border-t border-gray-800 text-center">
        <p className="text-xs text-slate-600">
          Auto-refreshing every 15s &middot; Powered by Prometheus &middot;{" "}
          <a
            href="https://github.com/MrGuato/pi-cluster"
            target="_blank"
            rel="noopener noreferrer"
            className="text-cyan-600 hover:text-cyan-400 transition-colors"
          >
            View on GitHub
          </a>
        </p>
      </footer>
    </main>
  );
}

