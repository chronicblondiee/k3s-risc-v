# Fixing Traefik and metrics-server on riscv64 (and a full board reflash along the way)

**Board:** Orange Pi RV2 (`k8s-rv2-01`, riscv64)
**Playbooks:** `playbooks/07_riscv64_klipper_helm.yml`, `playbooks/08_riscv64_klipper_lb.yml`, `playbooks/09_riscv64_metrics_server.yml`

> IPs shown below (`192.168.1.80`) are placeholder examples — real values
> live only in the gitignored `inventory.ini`/`host_vars/*.yml`.

## Why

`docs/2026-07-09-k3s-riscv64-source-build.md` and
`docs/2026-07-10-riscv64-local-registry.md` got k3s itself, `pause`, and a
local registry working on riscv64. Two gaps were flagged there as
unresolved: `traefik` and `metrics-server` stuck `ImagePullBackOff`, with
no riscv64 build upstream. Live diagnosis showed the picture was more
layered than "these two images lack riscv64 builds":

- Traefik's **own** image (`docker.io/library/traefik`) does publish
  riscv64. What was actually blocking it: `rancher/klipper-helm` (the
  image k3s's helm-controller uses to run the `helm install` Job for any
  HelmChart CR, including Traefik) has no riscv64 build at all — so the
  install Job itself never got far enough to pull Traefik's image.
- Once that was fixed and Traefik's pod came up, a **third** gap
  appeared that hadn't been visible before: `rancher/klipper-lb` (k3s's
  ServiceLB image, used for any `LoadBalancer`-type Service — Traefik's
  default Service is one) also has no riscv64 build.
- `metrics-server`'s own image genuinely has no riscv64 build, full stop.

Each needed a different fix, described below.

## 0. The board got reflashed mid-session

Before any of this, the board was wiped and reflashed with a fresh
Armbian SD card (default state — no user besides `root`, DHCP networking,
hostname still `orangepirv2`). Bootstrapping a fresh board turned up two
real bugs in the existing playbooks that had gone unnoticed because they
depended on state left over from a previous run:

- `playbooks/05_k3s_riscv64_build.yml` downloaded the Go SDK tarball and
  extracted it into `~/sdk`, but nothing ever created `~/sdk` first — it
  only worked before because that directory happened to already exist
  from an earlier manual setup. Fixed by adding an explicit
  `ansible.builtin.file` task before the extract step.
- `scripts/build` (the actual k3s compile) failed 25 minutes in with `no
  space left on device` — not the disk, but `/tmp`, which this Armbian
  image mounts as a ~3.9G `tmpfs` (RAM-backed). Building all of
  Kubernetes's packages fills that well before the real disk (51G free
  on the SD card) is anywhere near full. Fixed by pointing `TMPDIR`/
  `GOTMPDIR` at a directory on the real root filesystem instead of
  leaving Go's build scratch dir on its default (`/tmp`).

Also not previously automated: creating the initial admin user at all.
`playbooks/00_bootstrap_keys.yml` assumes the user already exists: on a
genuinely fresh board only `root` (default Armbian password) exists. That
one-time step — `useradd`, deploy the pubkey to `authorized_keys`,
passwordless sudo via `/etc/sudoers.d/` — was done by hand over the
default root password before `inventory.ini`/Ansible could take over.
`playbooks/02_base_config.yml`'s password-rotation tasks were tagged
`password_rotation` so they can be explicitly skipped
(`--skip-tags password_rotation`) when there's no vault set up yet and the
default credentials are being kept intentionally, rather than the
playbook hard-failing on missing vault variables.

## 1. `rancher/klipper-helm` → unblocks Traefik's helm-install jobs

Confirmed via Docker Hub's manifest list: `rancher/klipper-helm:v0.11.1-build20260615`
publishes `amd64`/`arm`/`arm64` only. k3s exposes a dedicated override for
this: `--helm-job-image` (`helm-job-image:` in `config.yaml`) — the same
mechanism already used for `pause-image`.

Built from the real upstream source
([`k3s-io/klipper-helm`](https://github.com/k3s-io/klipper-helm), pinned
to the exact tag k3s references) via `buildah`, natively on the board —
same "build on-device" approach as the k3s build itself. The upstream
`Dockerfile` needed two small patches to work on riscv64:

1. Its `GOARCH` case statement only handles `arm/v7`, `arm64`, `amd64` —
   added a `riscv64` branch.
2. Its helm-source-clone stage is `FROM alpine/git`, which itself has no
   riscv64 build (checked the same way) — swapped for plain `alpine:3.23`
   + `apk add git`, since that stage only ever needed `git clone`.

Getting `buildah` itself working on a bare board took three more rounds of
"checked directly, not assumed": it also needs `netavark` (network
namespace setup for `RUN` steps that need network access — `git clone`,
`apk add`, `go mod download`), `crun` (no default OCI runtime is bundled
at all — `buildah info` reported an empty `OCIRuntime` and failed with a
low-level `exec: no command` error without one), and `nftables` (`netavark`
shells out to the `nft` CLI). All three are ordinary Debian trixie riscv64
packages.

## 2. `rancher/klipper-lb` → unblocks ServiceLB (`svclb-*`) pods

Same manifest-list check, same result: no riscv64 build
(`amd64`/`arm`/`arm64` only). Unlike `pause-image`/`helm-job-image`, k3s
has **no dedicated flag** for this one — checked the source directly
(`pkg/cloudprovider/servicelb.go`'s `DefaultLBImage` constant, and
`pkg/daemons/control/deps/deps.go` where it gets optionally prefixed).
The only built-in override is `--system-default-registry`, which prefixes
**every** system image unconditionally — pause, klipper-lb, coredns,
local-path-provisioner, metrics-server, the Traefik chart, all of it —
with no fallback for images not present in that registry. Since coredns
and local-path-provisioner already pull fine as-is, that would have broken
working things to fix one broken thing.

Fixed instead with a **containerd-level registry mirror with fallback**
(see `templates/k3s-registries.yaml.j2`'s `mirror_docker_io` block):

```yaml
mirrors:
  "docker.io":
    endpoint:
      - "http://<registry_endpoint>"
      - "https://registry-1.docker.io"
```

containerd tries the local registry first for anything under `docker.io`,
and falls through to the real `docker.io` for anything not found there.
This only works because containerd's mirror substitutes the **host**, not
the path — so images fixed this way must be pushed to the local registry
under their **exact original repository path and tag**
(`rancher/klipper-lb:v0.4.17`, not a custom name), not the `imagename:riscv64-local`
convention used for `pause`/`klipper-helm` (which are referenced by their
own dedicated override flags instead, so the custom name doesn't matter
there).

`klipper-lb` itself needed no source compilation at all — its upstream
`Dockerfile` is just `alpine:3.23` (which does publish riscv64) plus
`iptables`/`ip6tables`/`nftables`/`iptables-legacy` from `apk` and a shell
script. Built and pushed as-is, no patches needed.

## 3. `rancher/mirrored-metrics-server` → fixes metrics-server

Confirmed via `registry.k8s.io`'s manifest list: `amd64`/`arm`/`arm64`/
`ppc64le`/`s390x`, no riscv64. metrics-server is deployed via a plain
static manifest (not a HelmChart — no `helm-install-metrics-server` job
ever existed), so it doesn't depend on the klipper-helm fix.

metrics-server's own release `Dockerfile` builds a `CGO_ENABLED=0` Go
binary and packages it onto `gcr.io/distroless/static`, which — like
`alpine/git` — has no riscv64 build either. Rather than patching that
Dockerfile and pulling in `buildah` again, this reused the simpler
`pause`-image pattern instead: build the binary natively (with the same
Go 1.26.2 riscv64 toolchain `playbooks/05_k3s_riscv64_build.yml` already
installs) and wrap it as a minimal OCI image with a small Python script —
`files/build_pause_image.py` generalized into
`files/build_static_binary_image.py` (parameterized binary path/entrypoint/
arch instead of hardcoding `pause`), since this is now the second time
this exact need has come up and won't be the last.

Pushed to the local registry under `rancher/mirrored-metrics-server:v0.8.1`
(its exact original path/tag) so the `docker.io` mirror from step 2 serves
it automatically — no live `kubectl set image` patch needed, and it
survives k3s re-applying its bundled static manifest on restart, since the
manifest still references the same image name either way.

## Result

```
$ kubectl get pods -A
NAMESPACE          NAME                            READY   STATUS      ...
kube-system        coredns-...                     1/1     Running
kube-system        helm-install-traefik-...         0/1     Completed
kube-system        helm-install-traefik-crd-...     0/1     Completed
kube-system        local-path-provisioner-...       1/1     Running
kube-system        metrics-server-...               1/1     Running
kube-system        svclb-traefik-...                2/2     Running
kube-system        traefik-...                      1/1     Running
riscv64-registry   riscv64-registry-...              1/1     Running
```

`kubectl top nodes` returns real data, and `curl http://<node-ip>/`
against Traefik returns its actual default 404 response (confirming it's
genuinely serving, not just `Running`).

## Published artifacts

All riscv64 build outputs from this session (k3s binary + systemd unit,
and the `pause`/`klipper-helm`/`klipper-lb`/`metrics-server` OCI images)
are published at
[`releases/riscv64-v1.36.2-k3s1`](https://github.com/chronicblondiee/k3s-risc-v/releases/tag/riscv64-v1.36.2-k3s1)
for quick re-provisioning without repeating the from-source builds.
