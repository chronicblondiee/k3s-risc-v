# k3s-risc-v

Ansible-managed home Kubernetes lab that started ARM-only, then pivoted
toward a mixed-architecture cluster by adding riscv64 boards. The original
arm64 board is currently offline after a hardware failure, so the active
scope is riscv64-only: `k8s-rv2-01`, `k8s-rv2-02`, and `k8s-rv2-03`.
Target distribution is [k3s](https://k3s.io/) across
the cluster — chosen because it's a single static binary and, unlike
microk8s, has a real from-source path to riscv64.

The repository was formerly named `k8s-arm`; it is now `k3s-risc-v` to match
the current k3s and RISC-V focus. The HA path is now live and automated for
three riscv64 servers; mixed-architecture work remains future scope.

See `AGENTS.md` for full node-by-node detail, hardware notes, and known
gotchas (it's written for AI coding agents working in this repo, but is
equally useful as a human reference), and `docs/` for narrative runbooks of
specific incidents and builds.

## Hardware

| Node | Board | Arch | OS | Status |
|---|---|---|---|---|
| `k8s-node-01` | Orange Pi 5 Plus (RK3588) | arm64 | Armbian, vendor kernel | **Offline — hardware failure** |
| `k8s-rv2-01` | Orange Pi RV2 | riscv64 | Armbian, nightly/trunk | Active, primary k3s server |
| `k8s-rv2-02` | Orange Pi RV2 | riscv64 | Armbian, nightly/trunk | Active k3s server |
| `k8s-rv2-03` | Orange Pi RV2 | riscv64 | Armbian, nightly/trunk | Active k3s server |

## Why this exists

k3s has no official riscv64 release. This repo documents (and automates)
building it from source natively on-board, including working around real
gaps found along the way — no riscv64 build of the `pause` sandbox image or
of `rancher/mirrored-library-busybox` (used by k3s's bundled
`local-path-provisioner`) exists anywhere upstream, so both get hand-built
or repointed at images that do support riscv64. See
`docs/2026-07-09-k3s-riscv64-source-build.md` for the full investigation,
every command run, and the reasoning behind each decision (e.g. why native
on-board build over cross-compiling, why `registry:3.0.0` over `2.x` for
the local image registry in `docs/2026-07-10-riscv64-local-registry.md`).

The same pattern now covers the rest of the single-node riscv64 stack:
Traefik's `klipper-helm`, ServiceLB's `klipper-lb`, metrics-server,
local-path-provisioner's helper busybox image, host/in-cluster benchmarking,
SpacemiT X60 IME proof tooling, and an internal SpacemiT `llama.cpp` HTTP
sidecar for quantized LLM smoke inference.

## Repo layout

```
ansible.cfg, requirements.yml                  - Ansible project config
inventory.ini.example                          - copy to inventory.ini (gitignored) with your own host/IP/user
group_vars/all/vault.yml.example               - copy to vault.yml (gitignored), ansible-vault encrypt
group_vars/all/cluster.yml.example             - copy to cluster.yml (gitignored), set k3s API DNS/VIP + release vars
group_vars/all/local.yml                       - wires ANSIBLE_BECOME_PASSWORD env var to become password (see .env.example)
host_vars/<hostname>.yml.example               - copy to <hostname>.yml (gitignored) with your own static IP/gateway
.env.example                                   - copy to .env (gitignored), set ANSIBLE_BECOME_PASSWORD to skip --ask-become-pass
playbooks/00_bootstrap_keys.yml                - create/bootstrap the admin user and deploy SSH key
playbooks/01_nvme_install.yml                  - migrate SD boot to NVMe (DESTRUCTIVE, see docs/ incident report first)
playbooks/02_base_config.yml                   - hostname, static IP, timezone, apt upgrade, password rotation
playbooks/03_harden_ssh.yml                    - disable password auth and root SSH login
playbooks/04_k8s_node_prep.yml                 - k3s node prep: swap/zram, modules, sysctls, persistent journal
playbooks/05_k3s_riscv64_build.yml             - build+install k3s from source on the riscv64 node, standalone
playbooks/06_riscv64_registry.yml              - shared OCI registry host + consumer mirror config for riscv64 nodes
playbooks/07_riscv64_klipper_helm.yml          - rebuild rancher/klipper-helm for riscv64 (unblocks Traefik's helm-install jobs)
playbooks/08_riscv64_klipper_lb.yml            - rebuild rancher/klipper-lb for riscv64 (unblocks svclb-* ServiceLB pods); also enables the docker.io mirror-with-fallback
playbooks/09_riscv64_metrics_server.yml        - rebuild metrics-server for riscv64
playbooks/10_riscv64_local_path_busybox.yml    - durable (mirror-based) fix for local-path-provisioner's busybox image, replacing the fragile live-patch approach
playbooks/11_riscv64_node_benchmark.yml        - host + in-cluster CPU/memory/storage/network benchmark for the riscv64 node
playbooks/12_riscv64_ime_go_benchmark.yml      - copy/run the SpacemiT X60 IME Go proof and fetch benchmark reports
playbooks/13_riscv64_llama_cpp_sidecar.yml     - build/deploy SpacemiT llama.cpp as an internal ClusterIP inference service
playbooks/14_k3s_riscv64_ha_servers.yml        - install/reconfigure three riscv64 k3s servers with embedded etcd; pins node networking
playbooks/15_riscv64_ha_vip.yml                - keepalived VIP for k3s_api_endpoint, real automatic failover, authenticated /readyz check
playbooks/16_riscv64_preload_critical_images.yml
                                                - preload/tag critical riscv64 system images on every server
playbooks/17_riscv64_spread_system_workloads.yml
                                                - scale/spread CoreDNS and Traefik across at least two nodes
playbooks/18_riscv64_backup_cluster_state.yml   - etcd snapshot + local-path PV archive fetches to gitignored backups/
playbooks/19_refresh_known_hosts.yml            - refresh repo-local gitignored SSH known_hosts from confirmed node addresses
templates/                                     - Jinja2 templates rendered by the playbooks above
files/                                         - static assets (hand-built riscv64 pause image source, generalized single-binary OCI image builder)
tools/                                         - hardware-recovery build scripts and X60 IME Go proof tooling
benchmarks/results/                            - fetched benchmark reports from playbooks 11/12
backups/                                       - gitignored etcd/PV backup captures from playbook 18
docs/                                          - incident logs / troubleshooting runbooks
```

## Prerequisites

- Ansible (`ansible-playbook`, `ansible-vault`) on your control machine.
- SSH access to your own boards, with a key already deployed (see
  `playbooks/00_bootstrap_keys.yml` for the first-run bootstrap, which
  still uses password auth since no key exists yet at that point).
- `gh` (GitHub CLI) is only needed if you're forking this to manage your own
  copy the same way; not required to just run the playbooks.

## Quickstart

None of `inventory.ini`, `host_vars/*.yml`, `group_vars/all/vault.yml`,
`.vault_pass`, or `.env` are committed — they hold your real hosts, IPs,
and credentials. Set them up from the provided templates:

```bash
cp inventory.ini.example inventory.ini
cp host_vars/k8s-node-01.yml.example host_vars/k8s-node-01.yml
cp host_vars/k8s-rv2-01.yml.example host_vars/k8s-rv2-01.yml
cp host_vars/k8s-rv2-02.yml.example host_vars/k8s-rv2-02.yml
cp host_vars/k8s-rv2-03.yml.example host_vars/k8s-rv2-03.yml
cp group_vars/all/cluster.yml.example group_vars/all/cluster.yml
cp group_vars/all/vault.yml.example group_vars/all/vault.yml
# edit all of the above with your real hostnames/IPs/usernames, then:
ansible-vault encrypt group_vars/all/vault.yml
echo 'your-chosen-vault-password' > .vault_pass && chmod 600 .vault_pass
```

For the three-server riscv64 HA path, first create the real DNS/VIP named by
`k3s_api_endpoint`, make sure all three RV2 boards are NVMe/SSD-rooted, then
run `00` through `04`, `06`, `10`, `14`, `15`, `16`, `17`, and `18` in that order.
Run `19` after board identity has been confirmed against the router's DHCP
reservations so Ansible can keep host-key checking enabled. `15` gives
`k3s_api_endpoint` a real `keepalived` VIP with automatic failover instead
of relying on a single node's IP. `06` seeds the
shared registry from the published riscv64 release artifacts before the new
servers try to pull through it; `07`-`09` remain source-rebuild fallbacks if
those artifacts are missing or stale. `16` preloads the critical system image
refs on every server so ordinary failover is less dependent on live registry
pulls. `17` spreads only lightweight stateless kube-system workloads
(CoreDNS and Traefik). `18` captures an etcd snapshot plus registry/LLM
local-path PV archives into local gitignored `backups/`. `11`-`13` are
optional benchmarks/IME/llama.cpp workloads. See `AGENTS.md` for
node-specific gotchas encountered along the way. Double-check
`playbooks/01_nvme_install.yml` against `docs/2026-07-07-nvme-install-brick-and-recovery.md`
before running it — it bricked a board once here.

### Running `become`-requiring playbooks without prompting every time

Playbooks that need `sudo` — e.g. the password-rotation tasks in
`02_base_config.yml` — normally prompt interactively via
`--ask-become-pass`. To avoid retyping it every run, copy `.env.example` to
`.env` (gitignored), fill in `ANSIBLE_BECOME_PASSWORD`, then:

```bash
# bash/zsh
set -a; source .env; set +a
ansible-playbook playbooks/02_base_config.yml --limit <host>
```

```fish
# fish does not source KEY=value files natively; set the var directly
# for the session instead, or use a plugin like bass/fenv if you want
# .env-file loading:
set -gx ANSIBLE_BECOME_PASSWORD 'your-password'
ansible-playbook playbooks/02_base_config.yml --limit <host>
set -e ANSIBLE_BECOME_PASSWORD
```

`group_vars/all/local.yml` resolves `ansible_become_password` from that
env var automatically at runtime; if it's unset, normal
`--ask-become-pass`/prompting behavior is unaffected.

## Security posture

This is a home-lab proof of concept, not a hardened deployment:

- `ansible-vault` protects `group_vars/all/vault.yml`, but the vault
  password itself (`.vault_pass`) sits in plaintext on disk next to it —
  fine for a single-operator home network, not appropriate if the threat
  model changes (e.g. a shared machine, or exposure beyond the LAN).
- The riscv64 registry (`playbooks/06_riscv64_registry.yml`) serves over
  plain HTTP with no auth — acceptable for a LAN-only home registry, not
  for anything internet-facing.
- Ansible SSH host-key checking is enabled and uses a repo-local
  gitignored `known_hosts` file. Refresh it with
  `playbooks/19_refresh_known_hosts.yml` only after node identity has been
  confirmed by stable MAC/DHCP reservation.
- SSH password auth and root SSH login are disabled once
  `playbooks/03_harden_ssh.yml` has run; access after that point is
  SSH-key-only.

See `AGENTS.md`'s "Vault" section and "Agent safety notes" for more detail,
including which operations are treated as destructive/hard-to-reverse
(SD/NVMe flashing, Maskrom recovery-mode writes).

## Current status / future work

- `k8s-node-01` (arm64) is offline due to a hardware failure. Future work:
  repair or replace it, add a new x86 node, and resume the
  mixed-architecture cluster goal after the riscv64-only HA cluster is stable.
- **The three-server riscv64 HA control plane is live as of 2026-07-12**
  (`k8s-rv2-01`, `k8s-rv2-02`, `k8s-rv2-03`, all `Ready` with
  `control-plane,etcd` roles) via `playbooks/14_k3s_riscv64_ha_servers.yml`,
  and `k3s_api_endpoint` (`k3s.home.arpa`) is backed by a real `keepalived`
  VIP (`playbooks/15_riscv64_ha_vip.yml`, `192.168.1.83`) with verified
  automatic failover. See `docs/2026-07-12-riscv64-ha-onboarding-rv2-02-03.md`
  for the onboarding process and `docs/2026-07-12-riscv64-k3s-ha-vip.md` for
  the VIP design and failover test.
- **Hardening follow-up applied 2026-07-13:** k3s node networking is pinned
  to inventory addresses, keepalived uses authenticated `/readyz`, critical
  riscv64 system images are preloaded on all three servers, CoreDNS and
  Traefik run two spread replicas, SSH host-key checking is re-enabled, and
  an etcd + registry/LLM local-path backup set was fetched to gitignored
  `backups/`. See `docs/2026-07-13-riscv64-ha-hardening.md`.
- `playbooks/06_riscv64_registry.yml` now separates the single registry host
  from registry consumers, pins the registry pod to the host node, and writes
  the same mirror-with-fallback config on all riscv64 nodes.
- `traefik` and `metrics-server` are now working on riscv64 — see
  `docs/2026-07-10-riscv64-traefik-metrics-server-fix.md`.
  `rancher/klipper-helm` (blocks Traefik's helm-install jobs) and
  `rancher/klipper-lb` (blocks any `LoadBalancer`-Service ServiceLB pod,
  including Traefik's) had no riscv64 builds upstream either and are now
  rebuilt via `playbooks/07_riscv64_klipper_helm.yml` and
  `playbooks/08_riscv64_klipper_lb.yml`; `metrics-server` itself
  (`playbooks/09_riscv64_metrics_server.yml`) likewise. Pre-built riscv64
  artifacts for all of these (plus k3s and pause) are published at
  [releases/riscv64-v1.36.2-k3s1](https://github.com/chronicblondiee/k3s-risc-v/releases/tag/riscv64-v1.36.2-k3s1)
  for quick re-provisioning without repeating the from-source builds.
- `playbooks/11_riscv64_node_benchmark.yml` records host and in-cluster
  benchmark results under `benchmarks/results/`; keep those separate from
  the IME-specific reports.
- `tools/x60-ime-go` and `playbooks/12_riscv64_ime_go_benchmark.yml` prove
  the SpacemiT X60 IME `vmadot` instructions can be driven from Go+cgo and
  now include matrix-shaped tiling benchmarks. This remains a proof/benchmark
  harness, not the production inference path.
- `playbooks/13_riscv64_llama_cpp_sidecar.yml` is the recommended path for
  real quantized LLM inference on the riscv64 node. It builds SpacemiT's
  `llama.cpp` runtime into the local registry, deploys it as a ClusterIP-only
  service, and validates `/health` plus `/v1/chat/completions`. See
  `docs/2026-07-12-riscv64-llama-cpp-sidecar.md` for the full troubleshooting
  record: corrected model URL, SpacemiT ONNX runtime dependency, same-tag
  image refresh, and `SPACEMIT_MEM_BACKEND=POSIX`.

## License

[MIT](LICENSE)
