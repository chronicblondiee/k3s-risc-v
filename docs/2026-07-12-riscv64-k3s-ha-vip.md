# Real floating VIP for the riscv64 k3s HA control plane (keepalived)

Replaces the single-point-of-failure `/etc/hosts` stopgap from
`docs/2026-07-12-riscv64-ha-onboarding-rv2-02-03.md` (which hardcoded
`k3s.home.arpa` to `k8s-rv2-01`'s IP) with a real `keepalived`-managed
VRRP floating IP that automatically moves to a healthy server if the
current holder goes down.

## Why keepalived over a router DNS entry

Two options existed: a static DNS/Pi-hole `A` record, or a real VIP.
A router DNS record isn't something this repo (or an agent working in it)
can execute — it requires the router's own admin UI, and even then it's
still one address someone has to manually repoint by hand if the server it
points at dies. `keepalived` is fully automatable via Ansible like
everything else here, and it delivers actual automatic failover: the VIP
moves to the next-healthiest server within a few seconds of the current
holder going down, no manual step required.

`keepalived` 2.3.3-1 is available directly from the Debian trixie riscv64
apt repo — unlike most of this project's other components, no repeat of
the "no riscv64 build exists anywhere" problem (`pause`, `klipper-helm`,
`klipper-lb`, `metrics-server`, `local-path-provisioner`'s busybox image
all needed hand-built riscv64 workarounds; keepalived just needed
`apt install`).

## Design

- New playbook: `playbooks/15_riscv64_ha_vip.yml`, targets `k3s_servers`.
- New template: `templates/keepalived.conf.j2`.
- New file: `files/check-k3s-api.sh` — local-only health check
  (`curl -k -s -o /dev/null -m 2 https://127.0.0.1:6443/readyz`), used as
  a `vrrp_script`. Deliberately does **not** use `curl -f`: an
  unauthenticated request to `/readyz` on this k3s build returns
  `401 Unauthorized` even when the API server is perfectly healthy (see
  gotcha below), so the check only distinguishes "reachable" from
  "connection refused/timeout" — which is exactly what a VIP failover
  trigger needs (is the local API process even listening), not full
  content validation.
- VIP address: `192.168.1.83` (next free address after `.80`–`.82`).
  The playbook preflight-pings it and aborts if anything answers, *unless*
  keepalived is already active locally (idempotency guard — after the
  first successful run the VIP is expected to answer, since it's ours).
- Priority-based election, `state BACKUP` on all three (no hardcoded
  MASTER, avoids split-brain races): `k8s-rv2-01=100`, `k8s-rv2-02=80`,
  `k8s-rv2-03=60` (derived from each node's index in `[k3s_servers]`).
  `track_script` applies `weight -30` on health-check failure, which
  always drops a degraded node below the next node's base priority
  (100→70 < 80; 80→50 < 60) — guarantees a clean handoff instead of a
  degraded node clinging to the VIP.
- `k3s_api_endpoint` itself is **unchanged** (`k3s.home.arpa`) — the
  playbook just repoints the existing `/etc/hosts` stopgap line at the VIP
  instead of `k8s-rv2-01`'s IP. TLS SAN validation is hostname-based and
  `k3s.home.arpa` was already in `k3s_api_tls_sans`, so nothing about the
  cert chain needed to change and k3s did not need restarting a third time
  this session.

New vars (`group_vars/all/cluster.yml` / `cluster.yml.example`):
`k3s_vip_address`, `k3s_vip_router_id`, `k3s_vip_auth_pass`.

## Gotcha: `/readyz` (and `/healthz`, `/livez`) return 401 for anonymous requests

First validation attempt used a plain `curl -k .../readyz` and asserted
HTTP 200 — this failed consistently, even hitting `127.0.0.1:6443`
directly on the node running k3s, unrelated to the VIP at all. This k3s
build/config rejects fully anonymous requests to these endpoints with
`401` rather than the "always-allow, no RBAC" behavior some kube-apiserver
setups have. **Fixed by validating with an authenticated client instead**:
`k3s kubectl --server=https://k3s.home.arpa:6443 get --raw=/readyz` using
the node's own `/etc/rancher/k3s/k3s.yaml` (client-cert auth) — returns
`ok`. This is arguably a better test anyway: it proves a real authenticated
client can reach and use the API through the VIP hostname, which is what
actual usage looks like.

## Verified failover

With the VIP held by `k8s-rv2-01`, stopped `keepalived` there
(`systemctl stop keepalived`, via `ansible ... -m systemd`). Within ~4
seconds the VIP appeared on `k8s-rv2-02` (`.83` showed as `secondary proto
keepalived` in `ip -o -4 addr show eth1`), and
`k3s kubectl --server=https://k3s.home.arpa:6443 get --raw=/readyz` from
`k8s-rv2-02` returned `ok` throughout — zero manual intervention needed.
Restarted `keepalived` on `k8s-rv2-01` afterward; it preempted the VIP
back (higher base priority), confirmed exactly one node held it
afterward and the cluster stayed healthy the whole time.

## VRRP virtual MAC, for the router's DHCP reservation

A floating VIP doesn't have one fixed MAC by default: keepalived just adds
it as a secondary address on the real interface, so ARP replies for it come
from whichever physical board currently holds it — the MAC changes across
failovers. That breaks a router DHCP reservation, which binds one IP to one
MAC permanently.

Fixed by enabling keepalived's VRRP virtual-MAC mode (`use_vmac` +
`vmac_xmit_base` in `templates/keepalived.conf.j2`): the VIP now lives on a
dedicated `vrrp.51@eth1` pseudo-interface with a constant MAC derived from
`virtual_router_id` (`00:00:5e:00:01:33` for id `51`, per the VRRP spec),
present identically on all three nodes regardless of which one currently
holds the VIP. `vmac_xmit_base` keeps the actual VRRP protocol
advertisements going out on the real interface (`eth1`) for reliability;
only ARP/gratuitous-ARP for the VIP address itself uses the virtual MAC.

Router-side reservation to add: **`192.168.1.83` → `00:00:5e:00:01:33`**.

One gotcha this introduced: the playbook's "which node holds the VIP" check
originally looked at `ip -o -4 addr show eth1` — after `use_vmac`, the VIP
moved to the new `vrrp.51` interface, so that check silently reported "no
one holds it" on every node even though the VIP was working correctly
(confirmed via the readyz check, which doesn't care which interface it's
on). Fixed by checking all interfaces (`ip -o -4 addr show`, no interface
filter) instead of assuming which one.

## Still outstanding

Router-side: add the reservation above, and exclude/reserve `.80`–`.82`
too if not already applied — same still-open item flagged in the
onboarding doc. Not something this repo can do; for the user to confirm at
the router admin UI.
