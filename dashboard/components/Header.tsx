import StatusDot from "./StatusDot";
import type { StatusLevel } from "@/lib/types";

interface HeaderProps {
  apiStatus: StatusLevel;
  apiDetail: string;
}

export default function Header({ apiStatus, apiDetail }: HeaderProps) {
  return (
    <header className="flex flex-col sm:flex-row items-start sm:items-center justify-between gap-4 mb-8">
      <div>
        <h1 className="text-2xl font-bold text-slate-100 tracking-tight">
          Pi Cluster <span className="text-cyan-500">//</span> Live Status
        </h1>
        <p className="text-xs text-slate-500 mt-1">
          k3s &middot; ARM64 &middot; GitOps with FluxCD
        </p>
      </div>
      <StatusDot status={apiStatus} label="API" detail={apiDetail} />
    </header>
  );
}

