# Local OCI registry for riscv64 image distribution

**Board:** Orange Pi RV2 (`k8s-rv2-01`, riscv64)
**Playbook:** `playbooks/06_riscv64_registry.yml`

> IPs shown below (`192.168.1.102`) are placeholder examples — real values
> live only in the gitignored `inventory.ini`/`host_vars/*.yml`.

## Why

`docs/2026-07-09-k3s-riscv64-source-build.md` covers building k3s from
source for riscv64 and hand-building a `pause` sandbox image, since no
riscv64 build of it exists anywhere upstream. That hand-built image lived
only in this one board's local containerd store — rebuilding it by hand on
every future riscv64 node that joins would repeat real, non-trivial work
(and repeat the manual OCI-image-hand-rolling process) for no reason. This
sets up a local registry so built/imported images only need doing once,
here, and future riscv64 nodes just pull from it — normal registry
behaviour, lower latency than re-fetching/rebuilding from upstream (or
building upstream doesn't even have) every time.

## Registry image: another riscv64 availability check

Same pattern as the `pause` image investigation — checked directly rather
than assumed:

- `registry:2.x` (any version checked: `2.8`, `2.8.3`): `amd64, arm64,
  arm, arm, ppc64le, s390x`. No riscv64.
- `registry:3.0.0` / `registry:3.0` (the CNCF Distribution v3 rewrite —
  actively maintained successor to `registry:2.x`, not a random fork):
  includes `riscv64`.

Pinned to `registry:3.0.0` (not `latest`) for reproducibility.

## Architecture

Deployed as a pod on the host in the `riscv64_registry_host` inventory group
(`k8s-rv2-01` in the current examples) rather than as a bare
systemd-managed container:

- Reuses `local-path-provisioner` for storage (already validated working
  as part of the base build) — a 4Gi PVC backed by `storageClassName:
  local-path`.
- Exposed via a `NodePort` Service on a fixed port (`30500`), so it's
  reachable from any host on the LAN at `<node-ip>:30500`, not just from
  pods inside this one node's cluster.
- Namespace: `riscv64-registry` (dedicated, not `default`/`kube-system`).
- The Deployment has a `kubernetes.io/hostname` nodeSelector so the
  local-path PVC remains attached to the intended registry host.

This deliberately dogfoods the cluster this repo just spent effort
proving works, rather than adding a second, separately-maintained service
(a plain container run via `ctr run` + a hand-written systemd unit) doing
the same job.

Manifest: `templates/riscv64-registry.yaml.j2` (Namespace + PVC +
Deployment + Service, rendered with `registry_namespace`,
`registry_image`, `registry_storage_size`, `registry_node_port` vars from
the playbook).

## containerd mirror configuration

For any riscv64 node (including the registry host itself) to actually pull images
*through* this registry instead of going straight to the upstream
registry, k3s's embedded containerd needs to know about it:
`templates/k3s-registries.yaml.j2` renders
`/etc/rancher/k3s/registries.yaml`:

```yaml
mirrors:
  "192.168.1.102:30500":
    endpoint:
      - "http://192.168.1.102:30500"
```

The `http://` scheme in the endpoint is what tells containerd this is a
plain-HTTP registry — there's no TLS in play at all here (home-lab,
LAN-only), so there's deliberately no `configs.tls` block; that's only
needed for HTTPS with an untrusted/self-signed cert, which doesn't apply
here. Requires a `k3s` restart to take effect (handled via the same
`notify: restart k3s` / `flush_handlers` pattern as
`playbooks/05_k3s_riscv64_build.yml`).

## Pushing what's already built

The hand-built `localhost/pause:riscv64-local` image (already sitting in
containerd's local store from the base build) gets tagged and pushed into
the new registry:

```bash
k3s ctr -n k8s.io --address /run/k3s/containerd/containerd.sock \
  images tag localhost/pause:riscv64-local 192.168.1.102:30500/pause:riscv64-local
k3s ctr -n k8s.io --address /run/k3s/containerd/containerd.sock \
  images push --plain-http 192.168.1.102:30500/pause:riscv64-local
```

`--plain-http` is passed explicitly on the `ctr push` itself too (belt and
braces alongside the `registries.yaml` mirror config — `ctr` as a raw
client doesn't reliably inherit the CRI-path mirror translation the same
way kubelet/containerd's CRI plugin does).

Verified via an actual HTTP round-trip against the registry's own API
(`GET /v2/pause/tags/list`), not just "the push command exited 0":

```
$ curl -s http://192.168.1.102:30500/v2/_catalog
{"repositories":["pause"]}
$ curl -s http://192.168.1.102:30500/v2/pause/tags/list
{"name":"pause","tags":["riscv64-local"]}
```

## A real bug found and fixed along the way: `kubectl patch` idempotency

While first deploying this, the PVC hung in `Pending` /
`ProvisioningFailed` — turned out to be the `local-path-provisioner`
riscv64-image gap documented in the main build doc (found *because of*
this registry work, fixed in `playbooks/05_k3s_riscv64_build.yml`, not
here).

Separately, a real idempotency bug was caught and fixed in this
playbook's sibling task in `05`: `kubectl patch` was assumed to report
`changed` only when it actually changed something, matching `kubectl
apply`'s `unchanged` convention. It doesn't — it always exits 0 and prints
`configmap/x patched`, appending `(no change)` only as a *suffix* when
nothing actually changed. The original `changed_when` used a substring
match on `"...patched"`, which matched both cases, so the task (and the
`local-path-provisioner` restart that depended on its `.changed` result)
incorrectly reported `changed` — and triggered an unnecessary restart —
on every single run. Fixed by checking for the absence of `"(no change)"`
instead. Caught by actually re-running the playbook twice and checking
for `changed=0`, not by inspection — worth doing that verification step
for any future playbook using `kubectl patch`.

## Open items / follow-ups

- The playbook now distinguishes the registry host from consumers, but the
  multi-node pull path still needs validation against the second and third
  physical RV2 boards once they are online.
- No TLS, no auth on the registry — acceptable for a LAN-only home-lab
  registry holding nothing sensitive (just OCI images this repo already
  built or could rebuild), consistent with this repo's existing "POC, not
  hardened" vault posture. Revisit if this ever needs to be reachable
  beyond the home network.
