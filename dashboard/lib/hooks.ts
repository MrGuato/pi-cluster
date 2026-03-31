"use client";

import useSWR from "swr";
import type { ClusterData } from "./types";

const fetcher = (url: string) => fetch(url).then((r) => r.json());

export function useClusterData() {
  return useSWR<ClusterData>("/api/cluster", fetcher, {
    refreshInterval: 15000,
  });
}

