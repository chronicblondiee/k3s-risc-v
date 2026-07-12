# RISC-V HA hardening: pinned networking, image preload, workload spread, backups

Follow-up hardening after the three-server riscv64 k3s control plane and
keepalived VIP were brought live on 2026-07-12.

## What changed

- `files/check-k3s-api.sh` now uses an authenticated readiness check:
  `/usr/local/bin/k3s kubectl --kubeconfig=/etc/rancher/k3s/k3s.yaml
  --request-timeout=2s get --raw=/readyz | grep -qx ok`.
  Anonymous `/readyz` still returns `401` on this cluster and is no longer
  accepted as the keepalived health target.
- `templates/k3s-config.yaml.j2` supports explicit `node-ip`,
  `advertise-address`, and `flannel-iface`. `playbooks/14_k3s_riscv64_ha_servers.yml`
  sets those from `ansible_host` and the default IPv4 interface, then
  validates that each node's Kubernetes `InternalIP` matches inventory.
- The VIP address is included in future k3s TLS SAN rendering via
  `k3s_vip_address`; hostname access through `k3s_api_endpoint` remains the
  primary supported path.
- `playbooks/16_riscv64_preload_critical_images.yml` pulls and tags the
  critical riscv64 system images on every k3s server:
  `pause:riscv64-local`, `klipper-helm:riscv64-local`,
  `rancher/klipper-lb:v0.4.17`,
  `rancher/mirrored-metrics-server:v0.8.1`, and
  `rancher/mirrored-library-busybox:1.37.0`.
- `playbooks/17_riscv64_spread_system_workloads.yml` scales CoreDNS and
  Traefik to two replicas and adds node-spread preferences. It intentionally
  leaves `metrics-server`, `local-path-provisioner`, the registry, and LLM
  workloads as singletons.
- `playbooks/18_riscv64_backup_cluster_state.yml` creates an etcd snapshot
  and fetches it to local gitignored `backups/`, along with local-path PV
  archives for the registry and LLM model data. PV ownership is discovered
  from Kubernetes and archives are created on the node that owns each
  local-path PV.
- `ansible.cfg` now enables SSH host-key checking and points Ansible at a
  repo-local gitignored `known_hosts` file. `playbooks/19_refresh_known_hosts.yml`
  refreshes that file from the confirmed inventory connection addresses.
- `.env.example` now documents `chmod 0600 .env`, and `backups/` is
  gitignored.

## Live validation

Commands below were run from the control machine with:

```bash
ANSIBLE_LOCAL_TEMP=/private/tmp/ansible-local \
ANSIBLE_REMOTE_TEMP=/tmp/.ansible-${USER}/tmp
```

Validation results:

- `playbooks/19_refresh_known_hosts.yml` scanned the confirmed node
  connection addresses and populated repo-local `known_hosts`.
- `playbooks/15_riscv64_ha_vip.yml` installed the authenticated health-check
  script. `k8s-rv2-01` held VIP `192.168.1.83`, and authenticated
  `/readyz` through `k3s.home.arpa` returned `ok` from all three servers.
- `playbooks/14_k3s_riscv64_ha_servers.yml` completed cleanly after applying
  pinned networking. All three nodes reported `Ready` and the expected
  `InternalIP` values:
  - `k8s-rv2-01` -> `192.168.1.80`
  - `k8s-rv2-02` -> `192.168.1.81`
  - `k8s-rv2-03` -> `192.168.1.82`
- The HA server playbook's pod/PVC smoke test passed.
- `playbooks/16_riscv64_preload_critical_images.yml` verified every critical
  image reference is cached locally on every server.
- `playbooks/17_riscv64_spread_system_workloads.yml` verified CoreDNS and
  Traefik each have two replicas across at least two nodes. At validation
  time both workloads were split across `k8s-rv2-02` and `k8s-rv2-03`.
- `playbooks/18_riscv64_backup_cluster_state.yml` fetched a complete backup
  set under `backups/20260712T231348/`:
  - etcd snapshot: 17M
  - registry PV archive: 161M
  - LLM model PV archive: 384M
  - PVC inventory: 693B

Syntax and whitespace checks passed for the touched playbooks:

```bash
ansible-playbook --syntax-check \
  playbooks/14_k3s_riscv64_ha_servers.yml \
  playbooks/15_riscv64_ha_vip.yml \
  playbooks/16_riscv64_preload_critical_images.yml \
  playbooks/17_riscv64_spread_system_workloads.yml \
  playbooks/18_riscv64_backup_cluster_state.yml \
  playbooks/19_refresh_known_hosts.yml

git diff --check
```

## Storage decision

Longhorn was considered but not added. It is the right class of storage
system for many k3s clusters, but this all-riscv64 cluster has repeatedly
hit missing upstream riscv64 image support. Longhorn's larger image set and
engine components would need a dedicated riscv64 feasibility spike before
being treated as production storage here. For now the cluster stays on
local-path storage plus explicit off-node backups.
