import { NextResponse } from "next/server";
import { queryPrometheus, queryPrometheusVector } from "@/lib/prometheus";
import type { ClusterData, NodeInfo } from "@/lib/types";

export const dynamic = "force-dynamic";

export async function GET() {
  try {
    // Fetch aggregate metrics in parallel
    const [cpuPercent, memoryPercent, pods, services] = await Promise.all([
      queryPrometheus(
        '100 - (avg(rate(node_cpu_seconds_total{mode="idle"}[5m])) * 100)'
      ),
      queryPrometheus(
        "(1 - (sum(node_memory_MemAvailable_bytes) / sum(node_memory_MemTotal_bytes))) * 100"
      ),
      queryPrometheus('count(kube_pod_status_phase{phase="Running"})'),
      queryPrometheus("count(kube_service_info)"),
    ]);

    // Fetch per-node data in parallel
    const [nodeCpu, nodeMem, nodeReady, nodePods, nodeBootTime, nodeRoles, nodeInfo] =
      await Promise.all([
        queryPrometheusVector(
          '100 - (avg by(instance)(rate(node_cpu_seconds_total{mode="idle"}[5m])) * 100)'
        ),
        queryPrometheusVector(
          "(1 - (node_memory_MemAvailable_bytes / node_memory_MemTotal_bytes)) * 100"
        ),
        queryPrometheusVector(
          'kube_node_status_condition{condition="Ready",status="true"}'
        ),
        queryPrometheusVector("count by(node)(kube_pod_info)"),
        queryPrometheusVector("node_boot_time_seconds"),
        queryPrometheusVector("kube_node_role"),
        queryPrometheusVector("kube_node_info"),
      ]);

    // Build instance → node name mapping from kube_node_info
    const instanceToNode: Record<string, string> = {};
    for (const n of nodeInfo) {
      const ip = n.metric.internal_ip;
      const name = n.metric.node;
      if (ip && name) {
        instanceToNode[ip] = name;
      }
    }

    function resolveNodeName(instance: string): string {
      const ip = instance.replace(/:\d+$/, "");
      return instanceToNode[ip] || instance;
    }

    // Build lookup maps
    const roleMap: Record<string, string[]> = {};
    for (const r of nodeRoles) {
      const name = r.metric.node;
      const role = r.metric.role;
      if (name && role) {
        if (!roleMap[name]) roleMap[name] = [];
        roleMap[name].push(role);
      }
    }

    const readyMap: Record<string, boolean> = {};
    for (const r of nodeReady) {
      if (r.metric.node) readyMap[r.metric.node] = r.value === 1;
    }

    const podsMap: Record<string, number> = {};
    for (const p of nodePods) {
      if (p.metric.node) podsMap[p.metric.node] = p.value;
    }

    const cpuMap: Record<string, number> = {};
    for (const c of nodeCpu) {
      cpuMap[resolveNodeName(c.metric.instance)] = c.value;
    }

    const memMap: Record<string, number> = {};
    for (const m of nodeMem) {
      memMap[resolveNodeName(m.metric.instance)] = m.value;
    }

    const bootMap: Record<string, number> = {};
    for (const b of nodeBootTime) {
      bootMap[resolveNodeName(b.metric.instance)] = b.value;
    }

    // Collect all known node names
    const allNodes = new Set<string>();
    for (const r of nodeReady) {
      if (r.metric.node) allNodes.add(r.metric.node);
    }
    for (const n of nodeInfo) {
      if (n.metric.node) allNodes.add(n.metric.node);
    }

    const now = Date.now() / 1000;

    const nodes: NodeInfo[] = Array.from(allNodes).map((name) => ({
      name,
      role: (roleMap[name] || ["worker"]).join(", "),
      ready: readyMap[name] ?? false,
      cpuPercent: Math.round((cpuMap[name] ?? 0) * 10) / 10,
      memoryPercent: Math.round((memMap[name] ?? 0) * 10) / 10,
      pods: podsMap[name] ?? 0,
      uptimeSeconds: bootMap[name] ? Math.floor(now - bootMap[name]) : 0,
    }));

    const data: ClusterData = {
      overview: {
        cpuPercent: Math.round(cpuPercent * 10) / 10,
        memoryPercent: Math.round(memoryPercent * 10) / 10,
        pods: Math.round(pods),
        services: Math.round(services),
      },
      nodes,
      timestamp: new Date().toISOString(),
    };

    return NextResponse.json(data);
  } catch (error) {
    console.error("Failed to fetch cluster metrics:", error);
    return NextResponse.json(
      {
        overview: { cpuPercent: 0, memoryPercent: 0, pods: 0, services: 0 },
        nodes: [],
        timestamp: new Date().toISOString(),
      },
      { status: 503 }
    );
  }
}

