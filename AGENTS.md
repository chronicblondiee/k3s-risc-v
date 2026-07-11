# k8s-arm

Ansible-managed home Kubernetes cluster. Started ARM-only (one node), then
began pivoting to a **mixed-architecture** cluster by adding a RISC-V node
alongside it. Repo/project name predates the RISC-V addition.

**Current focus: riscv64-only (`k8s-rv2-01`).** The original arm64 node
(`k8s-node-01`) is offline due to a hardware failure — see its section
below. Future work: repair/replace it and add a new x86 node, resuming the
mixed-architecture goal. Until then, no multi-node/mixed-arch work is in
scope.

## Target k8s distribution: k3s (cluster-wide decision)

Chosen over microk8s specifically because of the RISC-V node: microk8s is
snap-only, and the Snap Store API confirms there is no riscv64 channel at
all (only `amd64, arm64, ppc64el, s390x` — checked directly against
`api.snapcraft.io/v2/snaps/info/microk8s`, not just assumed). k3s is a
single static binary; there's no *official* riscv64 release either, but
unlike microk8s there's a real path to one — see the k8s-rv2-01 section
below for the from-source build being done for that node. This distro
choice applies cluster-wide, not just to the RV2 node.
`playbooks/04_k8s_node_prep.yml` (containerd/kubeadm-style prep) is closer
to what k3s needs than to microk8s, but still needs review before use (see
existing note below).

## Node: k8s-node-01 (currently offline — hardware failure)

**Status as of 2026-07-10: this board is out of service due to a hardware
failure.** It is not reachable and no playbooks should be run against it
until it's physically repaired/replaced. Future work: bring this node back
(repaired or replaced) alongside a new x86 node, at which point the
mixed-architecture cluster goal (currently riscv64-only, see `k8s-rv2-01`
below) resumes. Everything else in this section describes its last-known
working state, kept for reference.

- **Hardware:** Orange Pi 5 Plus (RK3588). Has onboard eMMC (233G), NVMe SSD
  (476.9G) attached, SPI-NOR flash (16M, holds the bootloader), and a microSD
  slot.
- **OS:** Armbian, **vendor/BSP kernel** (currently 6.1.115) — this is a hard
  requirement, not a preference. Mainline/current kernel does not have working
  GPU/VPU/NPU drivers for RK3588 yet. Do not suggest switching to mainline.
- **Distro flavor:** Ubuntu-based Armbian image ("resolute" codename), not
  Debian ("trixie"). Originally chosen for microk8s/snap compatibility — now
  moot since the cluster-wide target is k3s instead (see above), but not
  worth re-imaging this node over.
- **Target k8s distribution:** k3s (see cluster-wide decision above;
  supersedes the original microk8s plan). `playbooks/04_k8s_node_prep.yml`
  still needs review before use against this node — it predates both the
  microk8s and k3s decisions.
- **Network:** static IP `192.168.1.101` (example — real value lives in the
  gitignored `host_vars/k8s-node-01.yml`, see `.example` template), hostname
  `k8s-node-01`.
- **Login:** the admin user configured as `ansible_user` in the gitignored
  `inventory.ini` (SSH key auth once `00_bootstrap_keys.yml` has run; falls
  back to Armbian's default `1234` password pre-bootstrap).

## Node: k8s-rv2-01

- **Hardware:** Orange Pi RV2 (RISC-V, SoC family `spacemit`, board file
  reports `ky x1 orangepi-rv2`). No onboard eMMC actually populated (SoC has
  a non-removable mmc controller, `mmc2`, but it fails to initialize — no
  card present). SPI-NOR flash holds `fsbl`/`opensbi`/`uboot` (Armbian
  release is a **nightly/trunk build**, `26.8.0-trunk` as of 2026-07 — RISC-V
  support here is new; expect rough edges). No known Maskrom-equivalent
  hardware recovery path for this SoC — treat SPI/bootloader writes as
  effectively irreversible until one is identified.
- **Boot media: NVMe** (`/dev/nvme0n1p1`, Fanxiang S500Pro 512GB), migrated
  from the original SD card on 2026-07-08/09. Migration was done as a manual
  rootfs clone (partition + `mkfs.ext4` + `rsync -aHAX --one-file-system` +
  new filesystem UUID + updated `/etc/fstab` and `/boot/extlinux/extlinux.conf`
  on the NVMe copy only), **not** via `armbian-install`/`playbooks/01_nvme_install.yml`
  — that tool's "write bootloader to MTD flash" step is what bricked
  k8s-node-01 (see the incident doc), and this board boots via U-Boot's
  standard extlinux mechanism with root found by filesystem UUID, so no
  SPI/bootloader write was needed at all. The original SD card was never
  written to during migration and was simply pulled to test NVMe boot: it
  can still be reinserted as a known-good fallback (Armbian Debian install,
  hostname/keys/passwords already bootstrapped as of the SD-boot state at
  time of migration — network config still points at eth1/static IP
  `192.168.1.102`, example value, see note above).
  Do not reuse `01_nvme_install.yml` against this node without addressing the
  same concerns raised for k8s-node-01.
- **OS:** Armbian, latest Debian image (not Ubuntu — no microk8s/snap
  consideration here since the cluster target is k3s).
- **Target k8s distribution:** k3s, **built from source on this board**.
  No official upstream riscv64 k3s release exists (checked directly against
  the GitHub releases API for `v1.36.2+k3s1` — no riscv64 asset). The one
  community fork with prebuilt riscv64 binaries
  (`CARV-ICS-FORTH/k3s`) is stale — last release Oct 2024, Kubernetes
  v1.31.1 — which would be an unsupported version skew against a current
  server; rejected for that reason. k0s riscv64 is alpha-only, also
  rejected. Building from source natively on the board (not
  cross-compiling) sidesteps needing a riscv64 CGO cross-toolchain — see
  `docs/2026-07-09-k3s-riscv64-source-build.md` for the full build log,
  every command run, and the `yq` gotcha (Debian's `yq` package is a
  different, incompatible tool from the `mikefarah/yq` this build needs).
  **Build succeeded 2026-07-09, standalone server validated end-to-end
  the same day.** `scripts/build` produces a dev binary
  (`~/k3s/bin/k3s`, from `./cmd/server`) that is *not* the real shippable
  artifact — a separate step, `scripts/package-cli`, produces the actual
  self-contained one (`dist/artifacts/k3s-riscv64`, 88M, static,
  `CGO_ENABLED=0`) that's meant to be installed. `k3s --version` →
  `v1.36.2+k3s1 (01b6f04a)`, exact match to the target tag, no version
  skew. The first build attempt was lost to an unexplained board reboot
  mid-compile (root cause unconfirmed — volatile journal, no surviving
  logs); a monitored retry with normal memory headroom completed cleanly.
  Installed as a systemd service; hit one more gap — no riscv64 build of
  the `pause` sandbox image exists anywhere upstream (checked
  `rancher/mirrored-pause` and `registry.k8s.io/pause` across several
  versions) — fixed by compiling the (trivial) upstream `pause.c` natively
  on the board and hand-building a minimal OCI image for it, imported
  directly into k3s's local containerd store (no registry needed for
  single-node). After that, `kubectl get nodes` showed `Ready` and a real
  test pod (`busybox:latest`) ran, with working `kubectl logs`/`kubectl
  exec`, confirming actual riscv64 execution end-to-end. Full detail,
  including the reboot incident, the `scripts/build` vs `package-cli`
  gotcha, and the pause-image fix, in
  `docs/2026-07-09-k3s-riscv64-source-build.md`. Per explicit instruction,
  this node remains un-joined to `k8s-node-01` — no multi-node/mixed-arch
  cluster testing until separately instructed.
  **The whole sequence above is now automated and idempotent** as
  `playbooks/05_k3s_riscv64_build.yml` (verified via two consecutive runs
  reporting `changed=0`) — prefer running that over redoing any of this by
  hand. It also fixes a third instance of the same riscv64-image-gap
  pattern: `local-path-provisioner`'s helper pod defaults to
  `rancher/mirrored-library-busybox`, which (like `pause`) has no riscv64
  build; patched to use the Docker-official `busybox:1.37.0` instead,
  which does.
- **Local image registry:** `playbooks/06_riscv64_registry.yml` deploys a
  riscv64 OCI registry (`registry:3.0.0` — `registry:2.x` has no riscv64
  build, same check as `pause`) as a pod on this node's own standalone
  k3s, reachable at `192.168.1.102:30500` (example — see IP note above)
  from anywhere on the LAN. Exists
  so future riscv64 nodes can pull already-built images (starting with
  the hand-built `pause` image) instead of repeating the from-scratch
  build/hand-roll process. Does **not** yet distinguish "registry host"
  from "registry consumer" — running it against a second riscv64 node
  as-is would deploy a second independent registry, not point that node
  at this one; needs a small fix before a second riscv64 node is actually
  onboarded. Full detail in `docs/2026-07-10-riscv64-local-registry.md`.
- **Traefik / ServiceLB / metrics-server:** all three were previously
  ImagePullBackOff on this board with no riscv64 build upstream — now
  fixed. `rancher/klipper-helm` (blocked Traefik's helm-install Jobs
  before Traefik's own image was ever reached) and `rancher/klipper-lb`
  (blocked `svclb-*` ServiceLB pods for any `LoadBalancer` Service,
  including Traefik's) are rebuilt via `playbooks/07_riscv64_klipper_helm.yml`
  and `playbooks/08_riscv64_klipper_lb.yml`; `klipper-lb` specifically
  required introducing a containerd-level `docker.io` mirror-with-fallback
  (see `templates/k3s-registries.yaml.j2`) since k3s has no dedicated
  override flag for it, unlike `pause-image`/`helm-job-image`.
  `metrics-server` (`playbooks/09_riscv64_metrics_server.yml`) is rebuilt
  the same way as `pause` (native Go build + `files/build_static_binary_image.py`,
  a generalization of `files/build_pause_image.py`) and pushed under its
  exact original image path so the same mirror serves it. Full detail,
  including the three-tools-deep `buildah` dependency chain
  (`netavark`/`crun`/`nftables`, none installed by default) and the
  bugs found rebuilding k3s on a freshly reflashed board (missing SDK
  directory, `/tmp` as undersized `tmpfs`), in
  `docs/2026-07-10-riscv64-traefik-metrics-server-fix.md`. Pre-built
  artifacts published at
  [releases/riscv64-v1.36.2-k3s1](https://github.com/chronicblondiee/k3s-risc-v/releases/tag/riscv64-v1.36.2-k3s1).
- **`local-path-provisioner` busybox fix reverted itself:** the live
  `kubectl patch configmap local-path-config` fix from
  `playbooks/05_k3s_riscv64_build.yml` doesn't survive k3s re-applying
  its bundled addon manifest, which happened during this session's k3s
  restarts and silently broke the *next* PVC any workload tried to
  create. Fixed durably (same mirror pattern as klipper-lb/metrics-server
  above) in `playbooks/10_riscv64_local_path_busybox.yml` - if you see
  `rancher/mirrored-library-busybox` `ImagePullBackOff` again despite
  playbook 05 having run, this is why; re-run playbook 10, don't just
  re-patch the configmap by hand.
- **`kubectl` access:** works directly as the admin user, no sudo, from a normal
  interactive SSH login (`kubectl get nodes`, etc.) — `/usr/local/bin/kubectl`
  is a symlink to the multi-call `k3s` binary, and `~/.kube/config` is a
  user-owned copy of the kubeconfig with `KUBECONFIG` set in `~/.profile`
  (not `~/.bashrc` — see the doc for why). Same doc as above has the full
  setup and the gotcha where k3s's bundled `kubectl` ignores the standard
  `~/.kube/config` default unless `$KUBECONFIG` is set explicitly.
- **Network:** static IP `192.168.1.102` (example, see IP note above) on
  `eth1` (the board has two ethernet interfaces; `eth0` is unused/down),
  hostname `k8s-rv2-01`.
- **Login:** bootstrapped — the admin user (see `ansible_user` note above),
  SSH key auth only (password and root SSH login disabled by
  `03_harden_ssh.yml`).

## Repo layout

```
ansible.cfg, requirements.yml                  - Ansible project config
inventory.ini.example                          - copy to inventory.ini (gitignored) with your own host/IP/user
group_vars/all/vault.yml.example               - copy to vault.yml (gitignored), ansible-vault encrypt
group_vars/all/local.yml                       - wires ANSIBLE_BECOME_PASSWORD env var to become password (see .env.example)
host_vars/<hostname>.yml.example               - copy to <hostname>.yml (gitignored) with your own static IP/gateway
.env.example                                   - copy to .env (gitignored), set ANSIBLE_BECOME_PASSWORD to skip --ask-become-pass
playbooks/00-04_*.yml                          - see plan doc below for the intended sequence
playbooks/05_k3s_riscv64_build.yml             - build+install k3s from source on k8s-rv2-01, riscv64-only
playbooks/06_riscv64_registry.yml              - local OCI registry on k8s-rv2-01 for riscv64 image distribution
playbooks/11_riscv64_node_benchmark.yml        - CPU/memory/storage/network benchmark (sysbench+fio+iperf3, host + in-cluster), riscv64-only
benchmarks/results/                            - timestamped benchmark reports fetched back by playbook 11
templates/static-ip.yaml.j2                    - netplan template for 02_base_config.yml
templates/k3s-config.yaml.j2, k3s-registries.yaml.j2, riscv64-registry.yaml.j2
                                                - k3s/registry config for 05/06 (see those playbooks + docs/)
files/pause.c, files/build_pause_image.py      - hand-built riscv64 pause image assets for 05 (see docs/)
tools/                                         - build scripts for hardware-recovery tooling (see below)
docs/                                          - incident logs / troubleshooting runbooks, plus topical references
                                                 (benchmarks.md history, spacemit-x60-cpu-deep-dive.md CPU feature guide)
```

`inventory.ini`, `host_vars/*.yml`, `group_vars/all/vault.yml`, `.vault_pass`,
and `.claude/` are all gitignored — real host/IP/credential values never
leave this machine. IPs shown elsewhere in this file and in `docs/` are
placeholder examples, not the real LAN addresses.

No `roles/` — deliberately flat/minimal, now covering two nodes via
`host_vars/` rather than per-node playbook copies.

## Known-bad path: `armbian-install` on boards with both eMMC and NVMe

`playbooks/01_nvme_install.yml` automates Armbian's `armbian-install` /
`nand-sata-install` script via `ansible.builtin.expect` to migrate the
running SD-card system onto NVMe. **This was attempted live and bricked the
board.** Root cause and full recovery process: see
`docs/2026-07-07-nvme-install-brick-and-recovery.md`.

Short version: the install script has a latent bug where its `diskcheck`
shell variable becomes multi-line on boards that have *both* an eMMC and a
NVMe/SATA disk present (true for this board), and gets used unquoted in a few
places (e.g. `sfdisk --list-free /dev/$diskcheck`). Do not trust the
`expect`-automated version of this playbook on this hardware without a
supervised manual dry run first, and do not re-attempt the NVMe-boot path at
all without re-reading the incident doc.

## Vault (POC setup — not hardened)

`group_vars/all/vault.yml` is encrypted with `ansible-vault` and
`ansible.cfg` points `vault_password_file` at `.vault_pass` (gitignored, not
committed — never put its contents in a tracked file) so playbook runs
don't need `--ask-vault-pass` interactively. This is a proof-of-concept
shortcut for a home-lab setup, not real secrets hygiene: the vault password
lives in plaintext on disk right next to the thing it protects. Before this
cluster is exposed beyond the home network, or if this repo's threat model
changes, replace this with a real secret store (e.g. a password manager
entry read at runtime, not a file in the repo tree).

## Hardware recovery tooling (`tools/`)

`tools/build-rkdeveloptool.sh` and `tools/build-rk3588-loader.sh` rebuild the
two pieces needed to recover a bricked RK3588 board over USB in Maskrom mode:
Rockchip's `rkdeveloptool` CLI and a merged DDR-init/SPL loader blob. Neither
binary is committed to the repo (build artifacts, and `rkdeveloptool` needs a
macOS/Clang-specific build workaround) — run the scripts to reproduce them
locally. See the incident doc for the full recovery runbook and exact
`rkdeveloptool` command sequence used.

## Agent safety notes

- Anything that writes to `/dev/rdisk*` (SD card flashing), or invokes
  `rkdeveloptool db/wl/ef/gpt` (Maskrom flashing/erasing), is destructive and
  hard to reverse — always confirm the exact target device/storage with the
  user immediately before running it, even mid-task. Device identifiers
  (`diskN`) are not stable across sessions/replugs.
- `ansible-playbook` runs in this repo use `--ask-pass`/`--ask-become-pass`
  or `--ask-vault-pass` rather than storing credentials in the repo. Never
  add real passwords to any tracked file — only `vault.yml.example` with
  placeholders is committed.
