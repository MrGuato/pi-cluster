const PROMETHEUS_URL =
  process.env.PROMETHEUS_URL ||
  "http://kube-prometheus-stack-prometheus.monitoring.svc.cluster.local:9090";

interface PrometheusResult {
  metric: Record<string, string>;
  value: [number, string];
}

interface PrometheusResponse {
  status: string;
  data: {
    resultType: string;
    result: PrometheusResult[];
  };
}

export async function queryPrometheus(query: string): Promise<number> {
  const url = `${PROMETHEUS_URL}/api/v1/query?query=${encodeURIComponent(query)}`;
  const res = await fetch(url, { cache: "no-store" });
  const json: PrometheusResponse = await res.json();

  if (json.status !== "success" || !json.data.result.length) {
    return 0;
  }

  return parseFloat(json.data.result[0].value[1]);
}

export async function queryPrometheusVector(
  query: string
): Promise<Array<{ metric: Record<string, string>; value: number }>> {
  const url = `${PROMETHEUS_URL}/api/v1/query?query=${encodeURIComponent(query)}`;
  const res = await fetch(url, { cache: "no-store" });
  const json: PrometheusResponse = await res.json();

  if (json.status !== "success") {
    return [];
  }

  return json.data.result.map((r) => ({
    metric: r.metric,
    value: parseFloat(r.value[1]),
  }));
}

