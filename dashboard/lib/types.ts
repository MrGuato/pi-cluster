export interface ClusterOverviewData {
  cpuPercent: number;
  memoryPercent: number;
  pods: number;
  services: number;
}

export interface NodeInfo {
  name: string;
  role: string;
  ready: boolean;
  cpuPercent: number;
  memoryPercent: number;
  pods: number;
  uptimeSeconds: number;
}

export interface ClusterData {
  overview: ClusterOverviewData;
  nodes: NodeInfo[];
  timestamp: string;
}

export type StatusLevel = "healthy" | "warning" | "degraded" | "unknown";

