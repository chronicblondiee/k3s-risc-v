# Building k3s from source for riscv64 on k8s-rv2-01

**Date:** 2026-07-09
**Board:** Orange Pi RV2 (SpacemiT, riscv64), Armbian Debian trixie nightly

## Summary

No official k3s release supports riscv64 as of `v1.36.2+k3s1` (current
release, checked directly against the GitHub releases API — no
`k3s-riscv64` asset exists). The one community fork with prebuilt riscv64
binaries, `CARV-ICS-FORTH/k3s`, is stale: last release Oct 2024, tracking
Kubernetes v1.31.1. Running that as an agent against a current v1.36.x
server would be a 5-minor-version skew, outside Kubernetes' supported
n-3 policy — rejected for that reason (see "Options considered" below).

Decision: build k3s ourselves from source, natively **on the RV2 board
itself**, targeting the exact tag (`v1.36.2+k3s1`) intended for the arm64
server node, so there's no version skew at all. Building natively (rather
than cross-compiling from an amd64/arm64 host) sidesteps the need for a
riscv64 CGO cross-toolchain entirely — the board has a real gcc.

microk8s was also ruled out for this node: the Snap Store API
(`api.snapcraft.io/v2/snaps/info/microk8s`) lists only `amd64, arm64,
ppc64el, s390x` as channel architectures — no riscv64 edge/stable channel
exists at all, so there's nothing to install, stale or otherwise.

**Status as of this writing:** build succeeded. `~/k3s/bin/k3s` (238M) built
2026-07-09 20:56 UTC, `k3s --version` reports `v1.36.2+k3s1 (01b6f04a)` —
exact match to the target tag, no version skew. See "Build interrupted by
an unexplained reboot, then retried successfully" below for what happened
in between. Per explicit instruction, this node will **not** be joined to
`k8s-node-01` or any multi-node cluster yet — the immediate goal is only to
prove the from-source build works, run as a **standalone single-node** k3s
server on the RV2 board itself (embedded SQLite datastore, no external
etcd needed for one node).

## Options considered

1. **Stale `CARV-ICS-FORTH/k3s` fork** — fastest path (~10 min), but pins
   Kubernetes v1.31.1 (Oct 2024) against what will be a v1.36.x server:
   unsupported version skew, no security patches since Oct 2024, bundled
   containerd/runc also 2024-era. Rejected.
2. **k0s instead of k3s** — riscv64 support only exists in an alpha release
   (`v1.37.0-alpha.1+k0s.0`, changelog: "ci: Add support for
   linux-riscv64"), not in any stable release. Rejected as less mature than
   even the stale k3s fork.
3. **Build k3s from source ourselves** — chosen. See below.

## Why native on-board build, not cross-compile

k3s's CGO dependency (SQLite datastore, seccomp, apparmor) is **not
separable at build time**: `k3s`, `k3s-agent`, `k3s-server`, `kubectl`,
etc. are all the same compiled binary dispatching on `argv[0]` (see
`cmd/server`) — there's no way to build an "agent-only, no-CGO" variant.
`CGO_ENABLED=1` is hardcoded in `scripts/build`. That makes a riscv64 CGO
cross-toolchain unavoidable if cross-compiling from the Mac. Building
natively on the board avoids that problem completely: it already has a
real `gcc` (Debian trixie's riscv64 port), so CGO "just works" the same
way it would on any native Linux dev box.

## Prerequisites installed on k8s-rv2-01

Via ansible ad-hoc (using vault-stored credentials, since interactive
`sudo` needs a TTY that isn't available non-interactively):

```bash
ansible k8s-rv2-01 -m ansible.builtin.apt \
  -a "name=gcc,make,git,zstd,libsqlite3-dev,libseccomp-dev,libapparmor-dev,pkg-config state=present update_cache=true" \
  -e "@group_vars/all/vault.yml" \
  -e "ansible_become_pass={{ vault_admin_password }}"
```

All of these are present in Debian trixie's riscv64 archive without
needing backports — Debian's riscv64 port is mature enough that nothing
here required special-casing.

**Go 1.26.2 for `linux/riscv64`** — k3s's `go.mod` pins `go 1.26.2`.
Official Go binary distributions have shipped `linux-riscv64` (full CGO
support, not experimental) since Go 1.21:

```bash
curl -sL -o /tmp/go1.26.2.linux-riscv64.tar.gz \
  https://go.dev/dl/go1.26.2.linux-riscv64.tar.gz
mkdir -p ~/sdk && tar -C ~/sdk -xzf /tmp/go1.26.2.linux-riscv64.tar.gz
~/sdk/go/bin/go version   # go version go1.26.2 linux/riscv64
```

Installed under `~/sdk/go` (user-owned), not `/usr/local`, specifically to
avoid needing `sudo` for the rest of the build — none of the actual build
steps need root.

**`yq` — gotcha, read carefully:** k3s's `scripts/download` uses
`yq eval --no-doc .spec.chart` to extract Helm chart references from
manifests. Debian's own `yq` apt package is a *different, incompatible*
tool — it's the Python/jq wrapper (`kislyuk/yq`, v3.4.3 in trixie), which
has no `eval` subcommand at all. The script needs `mikefarah/yq` v4. That
project does publish a `yq_linux_riscv64` binary directly:

```bash
mkdir -p ~/bin
curl -sL https://github.com/mikefarah/yq/releases/latest/download/yq_linux_riscv64 \
  -o ~/bin/yq && chmod +x ~/bin/yq
~/bin/yq --version   # yq (https://github.com/mikefarah/yq/) version v4.53.3
```

Make sure `~/bin` is ahead of any system path entries so this `yq` wins.

## Build steps

```bash
cd ~
git clone --depth 1 --branch v1.36.2+k3s1 https://github.com/k3s-io/k3s.git
cd k3s

export PATH=$HOME/bin:$HOME/sdk/go/bin:$PATH   # our yq, our Go — ahead of system yq

ARCH=riscv64 GOARCH=riscv64 GIT_TAG=v1.36.2+k3s1 ./scripts/download
ARCH=riscv64 GOARCH=riscv64 GIT_TAG=v1.36.2+k3s1 ./scripts/build
```

`scripts/download` fetches, per `scripts/version.sh`'s existing (already
upstream, no patching needed) `riscv64)` case:

- `k3s-root-riscv64.tar` from `k3s-io/k3s-root` **v0.15.2** (released
  2026-06-10 — riscv64 support here is very recent; earlier k3s-root
  releases have no riscv64 asset at all). Checksum-verified via the
  `K3S_ROOT_SHA256` pinned in `scripts/version.sh`.
- `containerd` source at tag `v2.3.2-k3s2` from `k3s-io/containerd`
  (cloned, not a binary — containerd is compiled from source as part of
  this build).
- Helm charts (`traefik`, `traefik-crd`) referenced by k3s's bundled
  manifests.

`scripts/build` then compiles, natively, in order: CNI plugins, `runc`
(`v1.4.2`, k3s's pinned version), the containerd shim, and finally the
main `k3s` binary itself — all via plain `go build` with
`CGO_ENABLED=1` and tags `ctrd netcgo osusergo providerless
urfave_cli_no_docs sqlite_omit_load_extension apparmor seccomp`. No Docker,
no Dapper, no Vagrant involved — `scripts/build` is plain bash + `go
build`, runnable directly on the board.

Ran in the background (`nohup ... &`, logging to `/tmp/k3s-build.log` on
the board) since it's expected to take significant wall-clock time
compiling containerd/runc/k3s natively on an 8-core board.

## Board resources (checked before starting, for headroom)

- 8 cores, 7.7Gi RAM, 3.9Gi swap already active, 443G free on the NVMe
  root — comfortably enough for this build; no swap/disk provisioning
  needed beforehand.

## Build interrupted by an unexplained reboot, then retried successfully

The first `scripts/build` run got through the CNI-plugins compile stage
(`bin/cni`, produced 2026-07-08 23:35) and was confirmed still running
cleanly on a progress check shortly after (on the final `go build -o
bin/k3s ./cmd/server` step, downloading Go modules, no errors). At some
point after that, the board rebooted. No cause was ever confirmed:

- `/tmp/k3s-build.log` (the build's own output) did not survive — this
  board's journald is volatile/RAM-only (`/var/log/journal/` doesn't
  exist), and `/tmp` doesn't survive a reboot on this image either, so
  there was no log from the boot that crashed to inspect after the fact.
- `dmesg` after the reboot showed nothing but a completely normal fresh
  boot sequence (systemd/Bluetooth/fbcon init in the 13-33s-since-boot
  range) — no panic, no OOM-killer message, but that's expected either
  way since the previous boot's log is exactly what didn't survive.
- Thermal zones read a cool 40-41°C on the fresh boot, which rules out a
  thermal shutdown, but doesn't tell us anything about temps *during* the
  heaviest compile/link stage before the reboot.
- The board was then left for close to 24 hours before this was
  investigated further, but that time was **not** spent building — the
  build process had died with the reboot and nothing restarted it, so
  `bin/` sat unchanged (still just the k3s-root tools + `bin/cni`) the
  entire time.

The board was manually power-cycled/rebooted a second time by the human
operator (deliberate, not investigated further as a mystery). On restart,
memory/disk headroom was rechecked (7.3Gi free RAM, 3.9Gi swap idle, 438G
free disk — no sign of resource exhaustion at boot), and `scripts/build`
was re-run, this time actively monitored end-to-end (live polling of
`free -h` and the build log, plus a watch for the process disappearing or
SSH becoming unreachable) rather than left unattended. It completed
cleanly in about 6 minutes wall-clock this time (20:50-20:56) — much
faster than the first attempt, most likely thanks to warm Go build/module
caches left over from the interrupted run. Peak memory usage stayed well
within budget (never dropped below ~4.6Gi available out of 7.7Gi total).
The board stayed up the whole time (uptime continuous from the reboot
through well past build completion).

**Conclusion:** the reboot's root cause is unconfirmed and, given the
volatile journal, unrecoverable after the fact. It has not recurred on a
monitored retry with normal memory headroom, so it's treated as a
one-off rather than a reliably reproducible failure for now. If it
recurs, the next build attempt should enable persistent journald storage
first (`sudo mkdir -p /var/log/journal && sudo systemctl restart
systemd-journald`) so a repeat event actually leaves a diagnosable trail.

## Build result

`~/k3s/bin/` after the successful build:

- `k3s` — 238M, the single compiled binary (all CGO deps statically
  linked: SQLite datastore, seccomp, apparmor). `k3s --version` →
  `v1.36.2+k3s1 (01b6f04a)`, exact match to the target tag.
- `k3s-agent`, `k3s-server`, `k3s-token`, `k3s-etcd-snapshot`,
  `k3s-secrets-encrypt`, `k3s-certificate`, `k3s-completion`, `kubectl`,
  `crictl`, `ctr`, `containerd` — all symlinks to `k3s` (argv[0]
  dispatch, confirmed: this is not a build artifact gap, it's how k3s's
  build always works, per `scripts/build`'s own `k3s_binaries` array).
- `runc` — 11M, real separate binary (not a symlink).
- `containerd-shim-runc-v2` — 13M, real separate binary.
- `cni` — 4.6M, from the earlier CNI-plugins build stage (pre-dates the
  reboot, unaffected by it).
- `kubectl version --client` confirms `Client Version: v1.36.2+k3s1`.

## `scripts/build` vs `scripts/package-cli` — the dev binary is not the real artifact

`scripts/build` (used above) compiles `bin/k3s` from `./cmd/server`. This is
**not** the self-contained binary k3s actually ships — it's a dev-oriented
build that expects its sibling tools (CNI plugins, iptables, etc.) to be
found via `$PATH` next to it, and critically, the CNI plugin binaries
(`host-local`, `bridge`, `portmap`, `flannel`, etc.) don't exist as files at
all at this stage — they're meant to be symlinks to `bin/cni`, and those
symlinks are only created by a *separate* script, `scripts/package-cli`.

Running `bin/k3s server` directly (e.g. via a naive systemd unit pointing
`ExecStart` at it) fails at pod-sandbox-creation time with:

```
level=fatal msg="Error: failed to find host-local: exec: \"host-local\": executable file not found in $PATH"
```

`scripts/package-cli` is the real final step: it creates all the
`k3s_binaries`/`cni_binaries` symlinks, tars `bin/`+`etc/` into a
zstd-compressed blob, embeds that blob into `pkg/data/embed/`, and compiles
a *second*, fully self-contained binary from `./cmd/k3s`
(`CGO_ENABLED=0`, statically linked) at `dist/artifacts/k3s-riscv64`. This
second binary self-extracts its embedded `bin/` tarball to
`/var/lib/rancher/k3s/data/<hash>/bin` at first run — that's the actual
artifact an official k3s release ships, and it's what should be installed
to `/usr/local/bin/k3s`, not the `scripts/build` output.

```bash
export PATH=$HOME/bin:$HOME/sdk/go/bin:$PATH
ARCH=riscv64 GOARCH=riscv64 GIT_TAG=v1.36.2+k3s1 ./scripts/package-cli
# -> dist/artifacts/k3s-riscv64, 88M, static, CGO_ENABLED=0
```

This step is much faster than `scripts/build` (~1 minute here) since most
of the heavy compilation is already done and cached; it mainly re-links a
smaller wrapper package plus packages/embeds data.

## Standalone server: install, and the riscv64 `pause` image gap

Installed as a systemd service (binary from `dist/artifacts/k3s-riscv64`,
copied to `/usr/local/bin/k3s`; k3s's own bundled `k3s.service` unit copied
to `/etc/systemd/system/`; `systemctl daemon-reload && systemctl enable
--now k3s`). The server came up cleanly: sqlite datastore initialized,
certs generated, node registered.

`kubectl get nodes` showed the node `Ready` immediately. However, **every**
pod sandbox failed to create:

```
err="rpc error: code = NotFound desc = failed to pull image
\"rancher/mirrored-pause:3.6\": failed to pull and unpack image
\"docker.io/rancher/mirrored-pause:3.6\": no match for platform in
manifest: not found"
```

Checked multiple pause image sources for a riscv64 build — none exist:

- `rancher/mirrored-pause:3.6` (k3s's default): `amd64` (x7 variants),
  `arm64`, `arm`, `s390x`. No riscv64.
- `registry.k8s.io/pause` at `3.9`, `3.10`, `3.10.1` (upstream canonical
  image): `amd64`, `arm`, `arm64`, `ppc64le`, `s390x`, plus Windows
  variants. No riscv64 in any of these.

The `pause` binary itself is trivial (a ~60-line C program,
`kubernetes/kubernetes` at `build/pause/linux/pause.c` — just traps
SIGINT/SIGTERM and reaps zombies in a loop), so rather than wait on
upstream, built and packaged one ourselves, natively on the board:

```bash
# compile natively — board already has gcc from the k3s build prereqs
gcc -Os -static -DVERSION=3.10-riscv64-local -o pause pause.c

# wrap it in a minimal hand-built OCI image (no Docker/buildx needed):
# oci-layout + index.json + blobs/sha256/{manifest,config,layer} —
# single-layer tar containing just /pause, riscv64/linux platform.
# see build_pause_image.py in this repo's history / scratch if needed to
# reproduce; straightforward OCI Image Layout spec, ~80 lines of Python.

# import directly into k3s's own containerd (no registry needed for a
# single-node setup):
k3s ctr -n k8s.io images import pause-riscv64.oci.tar
# -> localhost/pause:riscv64-local, linux/riscv64, ~560KiB

# point k3s at it:
# /etc/rancher/k3s/config.yaml:
#   pause-image: localhost/pause:riscv64-local
systemctl restart k3s
```

After this, pod sandboxes created successfully. `coredns` and
`local-path-provisioner` (both multi-arch images with riscv64 variants)
came up `1/1 Running`. `helm-install-traefik*` and `metrics-server`
stayed `ImagePullBackOff` — those are separate, expected gaps: the
specific app images k3s's bundled manifests reference
(`rancher/mirrored-*`) don't publish riscv64 variants either. This is a
container-image-availability problem, not a k3s-build problem, and is
out of scope for what this doc set out to prove.

**End-to-end validation performed:**

```bash
kubectl get nodes
# k8s-rv2-01   Ready   control-plane   ...   v1.36.2+k3s1   ...   containerd://2.3.2-k3s2

kubectl run riscv64-test --image=busybox:latest --restart=Never -- \
  sh -c 'uname -a && echo hello-from-riscv64-k3s && sleep 3600'
kubectl logs riscv64-test
# Linux riscv64-test 6.18.35-current-spacemit ... riscv64 GNU/Linux
# hello-from-riscv64-k3s
kubectl exec riscv64-test -- uname -m
# riscv64
```

Node ready, pod scheduled, sandboxed, image pulled, container started,
`kubectl logs`/`kubectl exec` both worked. Test pod deleted afterward.
**This closes out the from-source riscv64 k3s build validation goal** —
server + containerd + runc + CNI, all self-built, all confirmed working
standalone on real riscv64 hardware.

## `kubectl` access for the admin user (no sudo)

By default only root can read the kubeconfig, and there's no `kubectl` on
`$PATH` for the login user. Two separate things needed fixing:

**1. No `kubectl` binary.** The packaged `k3s` binary is multi-call — it
dispatches based on `argv[0]`, the same trick used for the
`k3s-agent`/`k3s-server`/etc. symlinks inside the build tree (see
"`scripts/build` vs `scripts/package-cli`" above). So a plain symlink is
enough, no separate download needed:

```bash
ln -s /usr/local/bin/k3s /usr/local/bin/kubectl
```

`/usr/local/bin` is already on the admin user's default `$PATH` on this
image, so nothing else was needed for the binary itself.

**2. Kubeconfig is root-only.** `/etc/rancher/k3s/k3s.yaml` is `-rw-------
root root` by default (k3s's own install docs call this out —
`--write-kubeconfig-mode`/`--write-kubeconfig-group` exist specifically to
relax this, which we deliberately did **not** set, since that would make
the kubeconfig — i.e. full cluster-admin credentials — world-readable on a
multi-user box; not appropriate even for a home-lab POC). Instead, copied
it to a per-user location:

```bash
mkdir -p ~/.kube && chmod 700 ~/.kube
sudo cp /etc/rancher/k3s/k3s.yaml ~/.kube/config
sudo chown "$(whoami):$(whoami)" ~/.kube/config
chmod 600 ~/.kube/config
```

**Gotcha: k3s's bundled `kubectl` does not use the standard `~/.kube/config`
default.** Standard `kubectl` falls back to `~/.kube/config` when
`$KUBECONFIG` isn't set; k3s's built-in `kubectl` subcommand instead
hardcodes a fallback to `/etc/rancher/k3s/k3s.yaml` (visible in the
`Unable to read /etc/rancher/k3s/k3s.yaml, please start server with
--write-kubeconfig-mode...` warning it prints when that file isn't
readable). So `$KUBECONFIG` has to be set explicitly — pointing it at the
copy above does the trick:

```bash
# ~/.profile (NOT ~/.bashrc — see below)
export KUBECONFIG=$HOME/.kube/config
```

**Why `.profile` and not `.bashrc`:** Debian's default `~/.bashrc` starts
with a non-interactive guard (`case $- in *i*) ;; *) return;; esac`), so
anything appended to it is silently skipped for non-interactive shell
invocations. `~/.profile` is sourced unconditionally by login shells (and
itself sources `~/.bashrc` for the interactive case), so it's the right
place for an env var that should apply everywhere. Verified with `ssh
<user>@host 'bash --login -c "kubectl get nodes"'` (forces real
login-shell semantics) — works. A plain `ssh host "kubectl ..."` does
*not* pick it up, because SSH's non-interactive remote-command mode
doesn't invoke a login shell either — that's expected, not a bug in the
setup.

After both fixes, `kubectl get nodes` / `kubectl get pods -A` work
directly as the admin user, no `sudo`, from a normal interactive SSH
login.

## `local-path-provisioner`'s helper pod is another riscv64 image gap

Same class of bug as `pause`, found while deploying the local registry (see
`docs/2026-07-10-riscv64-local-registry.md`): any `PersistentVolumeClaim`
using the default `local-path` storage class hangs in `Pending` /
`ProvisioningFailed` ("create process timeout after 120 seconds"). Cause:
`local-path-provisioner`'s helper pod (which just runs `mkdir` on the host
path) defaults to `rancher/mirrored-library-busybox:1.37.0`, which — like
`rancher/mirrored-pause` — has no riscv64 build. Checked directly rather
than assumed:

- `rancher/mirrored-library-busybox:1.37.0`: `amd64, arm64, arm, arm, arm,
  s390x`. No riscv64.
- `busybox:1.37.0` (Docker-official, same version): includes `riscv64`.

Fixed by patching the `local-path-config` ConfigMap in `kube-system` to
point `helperPod.yaml` at `busybox:1.37.0` instead, then restarting
`local-path-provisioner` to pick it up. Automated in
`playbooks/05_k3s_riscv64_build.yml` now (runs after the node is Ready, so
it's the first thing that touches this — no PVC anywhere on this node
should ever hit the original bug again). Worth remembering as a pattern:
**any `rancher/mirrored-*` image is suspect on riscv64 until checked** —
this is the third instance found (`pause`, this, and `registry:2.x` in the
same investigation), all fixable by finding/building an actual multi-arch
riscv64 image instead.

## Automated: `playbooks/05_k3s_riscv64_build.yml`

The manual sequence documented above (prereqs → download → build →
package-cli → custom pause image → install → `kubectl` access → the
`local-path-provisioner` fix) is now a repeatable, idempotent Ansible
playbook — `ansible-playbook playbooks/05_k3s_riscv64_build.yml --limit
k8s-rv2-01`. Verified idempotent by running it twice back to back
(`changed=0` on the second run). Every expensive step (Go/yq install, git
clone, `scripts/download`/`build`/`package-cli`) is guarded so a re-run
after an interrupted build (e.g. another reboot) resumes rather than
redoing finished work — see the playbook's own header comment and
`files/pause.c` / `files/build_pause_image.py` / `templates/k3s-config.yaml.j2`
for the supporting assets. Image distribution for future riscv64 nodes
(so they don't need to repeat this whole build) is now handled separately
— see `docs/2026-07-10-riscv64-local-registry.md`.

## Open items / follow-ups

- **Done:** standalone single-node k3s server validated end-to-end,
  `kubectl` access for the admin user without sudo, the whole sequence turned
  into an idempotent playbook, and a local image registry for
  distributing built artifacts to future riscv64 nodes (see the sections
  above and `docs/2026-07-10-riscv64-local-registry.md`). Per standing
  instruction, this node remains un-joined to `k8s-node-01` — no
  multi-node/mixed-arch testing until separately instructed.
- Several bundled workloads (`traefik`, `metrics-server`) can't run on
  this node at all until their upstream images publish riscv64 variants —
  not something we can fix by building k3s differently. (Could in
  principle be worked around the same way `pause` was — hand-build or
  find a riscv64 image and push it through the local registry — but
  hasn't been done, since neither is required for the standalone
  validation goal.)
- This build output (`~/k3s/bin/k3s`, `dist/artifacts/k3s-riscv64`) is
  riscv64-specific and irrelevant to `k8s-node-01` — don't confuse it with
  whatever installs the official arm64 k3s release on that node (separate,
  still-pending task, currently paused per the "riscv-only focus"
  instruction).
