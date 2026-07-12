# Onboarding k8s-rv2-02 and k8s-rv2-03 into the riscv64 HA control plane

Brings the cluster from one riscv64 server (`k8s-rv2-01`) to the planned
three-server embedded-etcd topology. **Complete as of 2026-07-12** —
`playbooks/14_k3s_riscv64_ha_servers.yml` ran successfully and all three
nodes report `Ready` with `control-plane,etcd` roles.

## Board identity: verify by MAC, not by whatever IP it currently answers on

The two new boards were physically labeled `rv2-1`/`rv2-2` by hostname in
the router's DHCP reservation table (`.81`/`.82` respectively, alongside
`k8s-rv2-01`'s existing `rv2-0`/`.80`), but **the board initially reachable
at a given IP was not reliably the board meant for that IP** — one board
came up on a transient non-reserved DHCP lease (`.15`) while the other sat
on `.81` (a different board's reserved address) before its own reservation
had ever been exercised. Trusting "whichever board answers at the IP I
expected" led to provisioning a board as `k8s-rv2-03` when its NIC MAC
(`c0:74:2b:fb:14:71`) actually matched the router's `.81`/`rv2-1`
reservation — i.e. it was `k8s-rv2-02`. This produced a live IP collision
on `.82` once the real `rv2-2` board (MAC `c0:74:2b:fb:90:15`) came online
and both boards contended for that address (intermittent SSH host-key
flapping between two distinct, valid keys was the tell).

**Lesson: always confirm a board's identity by NIC MAC address
(`ip link show <iface>`) against the router's DHCP reservation table before
running any hostname/IP-changing playbook against it.** A board's current,
momentary IP is not proof of identity — only its MAC is stable.

Recovery: pushed a plain DHCP netplan config (with the required
`renderer: networkd` line — `02_base_config.yml`'s netplan-detection task
greps for that key specifically, so a hand-written netplan file needs it
too, or the playbook aborts with "No /etc/netplan/*.yaml found") to the
mislabeled board to release the contested address, waited for it to land
on its real DHCP-reserved IP, re-verified by MAC, then re-ran
`02_base_config.yml` with the corrected inventory hostname.

## SD→NVMe migration (manual clone, both boards)

Per the standing rule from
`docs/2026-07-07-nvme-install-brick-and-recovery.md`, `01_nvme_install.yml`
was not used. Same manual process as `k8s-rv2-01`, repeated on both boards:

1. `sudo parted -s /dev/nvme0n1 mklabel gpt mkpart primary ext4 2048s 100%`
2. `sudo mkfs.ext4 -F -L armbi_root /dev/nvme0n1p1`
3. Mount it, `sudo rsync -aHAX --one-file-system / /mnt/nvme_migrate/`
   (1.9G on both boards — matches source exactly)
4. Replace the old SD-root UUID with the new NVMe-partition UUID in
   `/mnt/nvme_migrate/etc/fstab` and
   `/mnt/nvme_migrate/boot/extlinux/extlinux.conf` (**use literal UUID
   strings in `sed`, not shell variables threaded through a nested SSH
   command** — variable interpolation silently failed once through the
   extra quoting layer, producing a no-op substitution that looked like it
   ran cleanly)
5. Unmount, `sudo shutdown -h now`
6. Physically remove the SD card (it's the *live* root while running, so
   this can only happen after a clean shutdown, never while powered on),
   power back on, verify `mount | grep ' / '` now shows `/dev/nvme0n1p1`

The SD cards were left untouched/unwiped as known-good fallbacks, same as
`k8s-rv2-01`'s.

**`k8s-rv2-03`'s NVMe drive was not blank** — it came with an existing EFI
System Partition + a 476.7G exFAT partition (label `Untitled`), i.e. it was
a previously-used drive (Samsung `MZVLB512HAJQ-000L7`), unlike
`k8s-rv2-02`'s fresh Fanxiang SSD. Confirmed with the user before wiping it
that nothing on that partition was needed.

## `k3s_api_endpoint` doesn't actually resolve on the LAN yet

`.env`'s own comment says `k3s.home.arpa` "must resolve on the LAN (router
DNS / Pi-hole / hosts file)" — none of those three were actually in place.
The primary's own init doesn't need to resolve its own endpoint
(`cluster-init: true` just binds locally), so this went unnoticed until the
two joining servers tried to dial `https://k3s.home.arpa:6443` to fetch CA
certs and fetch a join token, and both failed identically:

```
level=fatal msg="Error: preparing server: failed to bootstrap cluster data: failed to check if bootstrap data has been initialized: failed to validate token: failed to get CA certs: Get \"https://k3s.home.arpa:6443/cacerts\": dial tcp: lookup k3s.home.arpa: device or resource busy"
```

(`getent hosts k3s.home.arpa` returned NXDOMAIN — `rc=2` — on all three
nodes, confirming it's a genuine missing-record issue, not a transient
`systemd-resolved` hiccup, despite the misleading "device or resource busy"
wording from Go's resolver.)

**Fixed at the time with a stopgap**: added a static `/etc/hosts` entry
(`192.168.1.80 k3s.home.arpa`) on all three nodes via an ad-hoc
`ansible ... -m lineinfile` command. This hardcoded the primary's IP, making
it a single point of failure — if `k8s-rv2-01` went down, `k3s.home.arpa`
would stop resolving even though the other two etcd members (`.81`/`.82`)
were still healthy and could otherwise serve.

**Resolved properly the same day**: `playbooks/15_riscv64_ha_vip.yml` now
gives the cluster a real `keepalived` VIP (`192.168.1.83`) and repoints
this same `/etc/hosts` line at it instead — verified failover (stopped
`keepalived` on the VIP holder, confirmed the VIP moved to the next node
within seconds with zero manual intervention). See
`docs/2026-07-12-riscv64-k3s-ha-vip.md` for the full design and test.

## Result

| Board | IP | MAC | Boot storage | Role |
|---|---|---|---|---|
| `k8s-rv2-01` | `.80` | (primary, unchanged) | NVMe | control-plane, etcd (original primary) |
| `k8s-rv2-02` | `.81` | `c0:74:2b:fb:14:71` | NVMe | control-plane, etcd (joined 2026-07-12) |
| `k8s-rv2-03` | `.82` | `c0:74:2b:fb:90:15` | NVMe | control-plane, etcd (joined 2026-07-12) |

`playbooks/14_k3s_riscv64_ha_servers.yml`'s own validation pass confirmed
all three nodes `Ready`, no `ImagePullBackOff`/`ErrImagePull` in
`kube-system`, and a throwaway PVC-backed pod bound and read back its data
successfully.

Both follow-ups closed out the same day: the `/etc/hosts` stopgap was
replaced with a real `keepalived` VIP (`docs/2026-07-12-riscv64-k3s-ha-vip.md`),
and the router's DHCP reservations for `.80`–`.83` were confirmed applied
(`rv2-0`/`rv2-1`/`rv2-2`/`rv2-vip`, all under the `Default` scope) —
closing the risk of a repeat `.82`-style collision.
