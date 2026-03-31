import InfraCard from "./InfraCard";

export default function InfrastructureStack() {
  return (
    <section className="mb-8">
      <h2 className="text-xs font-semibold text-slate-500 uppercase tracking-widest mb-4">
        Infrastructure Stack
      </h2>
      <div className="grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-5 gap-3">
        <InfraCard
          icon={<span>🖥️</span>}
          label="Operating System"
          value="Raspberry Pi OS"
          sublabel="ARM64 / Debian Bookworm"
        />
        <InfraCard
          icon={<span>💾</span>}
          label="Storage"
          value="local-path"
          sublabel="k3s default provisioner"
        />
        <InfraCard
          icon={<span>🌐</span>}
          label="CNI"
          value="Flannel"
          sublabel="VXLAN overlay"
        />
        <InfraCard
          icon={<span>⚖️</span>}
          label="Load Balancer"
          value="Traefik"
          sublabel="Ingress controller"
        />
        <InfraCard
          icon={<span>🔄</span>}
          label="GitOps"
          value="FluxCD"
          sublabel="Continuous reconciliation"
        />
      </div>
    </section>
  );
}

