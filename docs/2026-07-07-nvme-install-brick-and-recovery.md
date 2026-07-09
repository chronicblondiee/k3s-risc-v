# Incident: NVMe install bricked k8s-node-01, recovered via Maskrom

**Date:** 2026-07-07
**Board:** Orange Pi 5 Plus (RK3588), Armbian vendor kernel 6.1.115

## Summary

Attempted to migrate the running Armbian system from SD card to the onboard
NVMe SSD using Armbian's `armbian-install` tool, targeting full NVMe-only
boot (bootloader on SPI flash, root+`/boot` on NVMe). The install completed
its steps but left the board unable to boot from SD, NVMe, or SPI. Recovered
using the RK3588's Maskrom hardware recovery mode and Rockchip's
`rkdeveloptool`, by erasing the SPI flash so the boot ROM falls through to
another boot device. Ultimately abandoned the NVMe-boot goal for now and
pivoted to writing a full OS image directly to the onboard eMMC over USB,
since the physical SD card was lost (dropped, unrecoverable) partway through
re-attempting a simpler SD-based recovery.

## Timeline

### 1. Planning assumptions turned out wrong

The original plan (a local design doc, not tracked in this repo) assumed
this board had no eMMC. Live inspection before running the install
(`findmnt`, `lsblk`, `cat /proc/mtd`) showed it actually has:

- SD card (`mmcblk1`, then-current root)
- **Onboard eMMC** (`mmcblk0`, 233G, blank/unpartitioned)
- SPI-NOR flash (`mtd0`, 16M, `"loader"`)
- NVMe SSD (`nvme0n1`, 476.9G, with one pre-existing unlabeled ext4
  partition — not a blank disk)

### 2. Found a real bug in `armbian-install` for this hardware combo

Pulled the live script (`/usr/bin/armbian-install`, symlinked from
`nand-sata-install`) and traced its menu-construction logic by hand. For
this exact hardware (SD root + eMMC + SPI + NVMe), menu option **4** ("Boot
from MTD Flash - system on SATA, USB or NVMe") is the correct choice for
full NVMe-only boot — it copies rootfs+`/boot` to NVMe and writes U-Boot to
SPI.

However: the script's `diskcheck` shell variable
(`config/sources/.../armbian-install`, function-scope in `main()`) becomes
**multi-line** ("`mmcblk0\nnvme0n1`") whenever a board has both an eMMC and a
separate NVMe/SATA disk, because it's built via `lsblk | grep -E
'^sd|^nvme|^mmc' | grep -v <root>` with no dedup/single-select logic for
"other disk besides eMMC". This multi-line value gets used **unquoted** in
places like `sfdisk --list-free /dev/$diskcheck`, which under bash
word-splitting can turn into a malformed command (e.g. an extra bogus
positional argument). This is a latent upstream bug affecting any RK3588 (or
similar) board with both eMMC and NVMe present — not specific to this exact
Armbian version.

**Decision at the time:** because of this, declined to trust the
`ansible.builtin.expect`-automated version of the install
(`playbooks/01_nvme_install.yml`) for this run, and had the user drive
`armbian-install` interactively instead, with step-by-step guidance for each
dialog screen (this doc's author walked through the script source to predict
each screen in advance).

### 3. Terminal issue during the manual run (unrelated, minor)

First attempt showed a blank/dead screen when running `armbian-install`.
Cause: local terminal was Kitty (`TERM=xterm-kitty`), and the Armbian minimal
image doesn't have Kitty's terminfo entry installed, so `ncurses`/`dialog`
couldn't initialize. Fixed with `TERM=xterm-256color armbian-install` (or
`TERM=linux` as a further fallback).

### 4. The install run and the brick

Walked through: option 4 → **Skip** the "wipe all partitions" auto-recommend
dialog (deliberately, to sidestep the multi-disk `diskcheck` bug above and
instead directly select+reuse the NVMe's existing partition) → selected the
one existing `nvme0n1p1` partition as destination → confirmed erase → chose
**ext4** (not btrfs — other RK3588/Orange Pi 5 Plus users report btrfs
causing NVMe boot failures) → rootfs copy completed → confirmed "write
bootloader to MTD Flash" (yes, twice, including the final SPI write
confirmation) → selected **Power off** at the final prompt.

After power-off, physically removing the SD card, and powering back on: the
board was fully bricked. No bootloader-stage LED activity, no boot from SD,
no boot from NVMe, not reachable on the network.

**Root cause (inferred, not fully confirmed):** the bootloader write to SPI
flash almost certainly completed in a broken/incomplete state — possibly a
downstream effect of the `diskcheck` bug above corrupting some step earlier
in the run even though we routed around its most obvious trigger point, or
an unrelated failure in the MTD write itself. Not root-caused further since
recovery (below) didn't require knowing the exact mechanism.

### 5. Failed simple recovery attempt

Reformatted the SD card (FAT32) intending to reflash a fresh image and retry
booting from SD — did not fix the brick. In hindsight this was never going
to work on its own: if SPI now holds higher boot priority with an
invalid/corrupt image, the boot ROM may hang or fail before ever consulting
the SD card, regardless of what's on it.

### 6. Maskrom recovery

RK3588 has a hardware Maskrom mode that lets a USB host reflash the chip
regardless of what's in any storage device — this is what actually fixed it.

**Physical procedure for this board** (Orange Pi 5 Plus labels the trigger
"Recovery", used interchangeably with "MaskROM" in Orange Pi's own docs):

1. Board fully powered off and unplugged.
2. Press and hold the **Recovery** button.
3. While still holding it, connect power to the **DC_IN** Type-C port.
4. Keep holding ~2 seconds after power connects, then release.
5. Connect a USB-C data cable from the **other Type-C port — the one next to
   the Recovery button** — to the host machine. (DC_IN is power-only; the
   port next to Recovery is the data/OTG port. Mixing these up looks
   identical to "nothing happens" and was the first failure mode hit here.)

**Tooling built on macOS (Apple Silicon) to talk to it:**

- `rkdeveloptool` (Rockchip's official open-source flashing CLI) — has no
  Homebrew formula, built from source
  (github.com/rockchip-linux/rkdeveloptool). Apple Clang treats a
  variable-length-array usage in the upstream code as a hard error under
  `-Werror` (`-Wvla-cxx-extension`) where GCC would only warn; build with
  `-Wno-error` appended to `CXXFLAGS` to get past it. See
  `tools/build-rkdeveloptool.sh`.
- A merged RK3588 DDR-init/SPL loader blob (needed for `rkdeveloptool db`
  before most other commands work) — built from Rockchip's `rkbin` repo
  (github.com/rockchip-linux/rkbin) using its `tools/boot_merger` against
  `RKBOOT/RK3588MINIALL.ini`. `boot_merger` is an x86_64 Linux ELF binary
  with no macOS build; ran it inside a `debian:bookworm`
  `--platform linux/amd64` Docker container instead of trying to get a
  native build working. See `tools/build-rk3588-loader.sh`.

**Gotcha:** on this machine, the agent's own shell had no raw USB access —
`system_profiler SPUSBDataType` and `ioreg -p IOUSB` both returned
completely empty output even for basic queries, unrelated to what was
plugged in. This looked exactly like "board not detected" but wasn't — it
was the shell environment, not the hardware. Running with an explicit
unsandboxed-execution flag resolved it. If USB tooling appears to see
nothing, sanity-check basic USB enumeration (any device, not just the target
board) before concluding it's a hardware/cabling problem.

**Recovery commands actually run** (after confirming detection via
`rkdeveloptool ld` → `Vid=0x2207,Pid=0x350b ... Maskrom`):

```bash
rkdeveloptool db rk3588_spl_loader_v1.21.114.bin   # load bootstrap loader into RAM
# rkdeveloptool ld immediately after this can still show "Maskrom" (stale
# label) even though db succeeded - confirm via a command that actually
# talks to the chip instead, e.g.:
rkdeveloptool rci    # chip info - succeeding confirms the loader is really running
rkdeveloptool rcb    # capability
rkdeveloptool rid    # flash id of whatever storage is currently selected

rkdeveloptool cs 9    # select SPINOR as target storage (1=EMMC, 2=SD, 9=SPINOR)
rkdeveloptool rid     # -> "NOR  " - confirms we're targeting the right chip
rkdeveloptool rfi     # -> Samsung, 16MB, confirms it's the SPI flash
rkdeveloptool ppt     # -> printed a normal-looking GPT table (idbloader/vnvm/
                      #    reserved_space/reserved1/uboot_env/reserved2/uboot) -
                      #    the table parses fine; the actual bootloader *content*
                      #    is what's presumed broken, not the partition table

rkdeveloptool ef      # ERASE FLASH - wipes SPI entirely. Destructive; explicit
                      # user go-ahead obtained before running.
rkdeveloptool ppt     # -> "Not found any partition table!" - confirms SPI is
                      # now blank. Boot ROM should skip SPI and fall through to
                      # the next boot device on its search order.
```

### 7. SD card lost mid-recovery

Plan after erasing SPI was to reflash the (reformatted) SD card and boot from
that. The physical SD card was then dropped and not recovered, so this path
was abandoned.

### 8. Current plan: write directly to eMMC over USB instead

Since Maskrom/`rkdeveloptool` access to the board is still available and SPI
is confirmed blank, the plan going forward (not yet executed as of this
writing — see repo/task state for current status) is:

```bash
rkdeveloptool cs 1               # select eMMC as target storage
rkdeveloptool wl 0 <image.img>   # write the full decompressed Armbian image
                                  # starting at sector 0 - equivalent to `dd`
                                  # to a disk, but over USB via Maskrom
```

Image: the **Ubuntu-based** vendor image
(`Armbian_26.5.1_Orangepi5-plus_resolute_vendor_6.1.115_minimal.img.xz`, not
the Debian "trixie" one) — the node will run **microk8s**, which wants an
Ubuntu base. This makes eMMC fully self-contained (own bootloader + rootfs)
with no SD card involved at all; with SPI blank, the boot ROM should fall
straight through to eMMC.

## Open items / follow-ups

- `playbooks/01_nvme_install.yml` should not be re-run as-is against this
  board without addressing the `diskcheck` bug above (upstream fix, or a
  workaround that pins a single disk explicitly rather than trusting the
  script's auto-detection).
- The NVMe SSD's original pre-existing partition/data (from before any of
  this) was never identified — it was ext4, unlabeled, contents unknown, and
  got wiped during the install attempt. Not believed to be important, but
  noting since it was never explained.
- `playbooks/04_k8s_node_prep.yml` was written for a generic
  containerd+kubeadm-style node prep, before microk8s was decided as the
  target k8s distribution. Needs reconciling with microk8s's own bundled
  containerd before use.
- Root cause of the original SPI corruption was never fully confirmed (see
  step 4) — if this recurs, worth capturing `armbian-install`'s log
  (`/var/log/armbian-install.log`) before wiping anything next time.
