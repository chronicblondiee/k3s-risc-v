# Onboarding k8s-rv2-02 and k8s-rv2-03 into the riscv64 HA control plane

Brings the cluster from one riscv64 server (`k8s-rv2-01`) to the planned
three-server embedded-etcd topology. Both new boards are now bootstrapped,
base-configured, and migrated to NVMe boot; `03`/`04`/`06`/`10` and the
actual HA join (`playbooks/14_k3s_riscv64_ha_servers.yml`) are still
pending as of this writing.

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

## Result

| Board | IP | MAC | Boot storage | Playbooks completed |
|---|---|---|---|---|
| `k8s-rv2-01` | `.80` | (primary, unchanged) | NVMe | full HA join already live |
| `k8s-rv2-02` | `.81` | `c0:74:2b:fb:14:71` | NVMe | `00`, `02` |
| `k8s-rv2-03` | `.82` | `c0:74:2b:fb:90:15` | NVMe | `00`, `02` |

Remaining: `03_harden_ssh.yml`, `04_k8s_node_prep.yml`,
`06_riscv64_registry.yml`, `10_riscv64_local_path_busybox.yml` on both new
boards, then `playbooks/14_k3s_riscv64_ha_servers.yml` for the actual
three-server embedded-etcd join (topology is now a valid odd count of 3).

Also worth doing before the HA join: add explicit DHCP reservations/pool
exclusions for `.80`–`.82` if not already fully applied on the router side
(a pending "Apply" was visible on the reservation table mid-session) to
prevent a repeat of the `.82` collision.
