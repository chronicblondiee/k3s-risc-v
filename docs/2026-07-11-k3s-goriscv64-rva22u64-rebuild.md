# 2026-07-11: Rebuilding k3s with GORISCV64=rva22u64

Follow-through on the cheapest win identified in
[spacemit-x60-cpu-deep-dive.md](spacemit-x60-cpu-deep-dive.md): the X60
implements the RVA22 profile, but `playbooks/05_k3s_riscv64_build.yml` only
set `GOARCH=riscv64`, so every Go binary in the k3s stack (k3s itself,
containerd-shim, runc, CNI plugins) targeted the rva20u64 baseline and never
used the Zba/Zbb bit-manipulation instructions this core has.

## Before state (proof the flag was missing)

```
$ go version -m /usr/local/bin/k3s | grep GORISCV64
	build	GORISCV64=rva20u64
```

## The change

One env var in playbook 05's `k3s_build_env` (commit `d902c49`):

```yaml
GORISCV64: rva22u64
```

`rva23u64` was deliberately NOT used: RVA23 assumes Zfa/Zawrs/Zimop, which
the X60 lacks — an RVA23-built binary can hit illegal-instruction faults on
this core. See the deep-dive doc for the full extension inventory.

## Forcing the rebuild — and the trap we hit

Playbook 05's build tasks are guarded with `creates:` on the build outputs,
so changing the env alone does nothing. The obvious force —
`rm ~/k3s/bin/k3s ~/k3s/dist/artifacts/k3s-riscv64` — **cost us a 33-minute
compile**, and the failure is worth recording:

1. k3s's `scripts/build` starts by cleaning old outputs with
   `[ -f "$i" ] && rm -f "$i"` over `bin/k3s-agent`, `bin/kubectl`,
   `bin/crictl`, etc. Those are **symlinks to `bin/k3s`**.
2. With `bin/k3s` deleted, the symlinks were *dangling*, `[ -f ]` (which
   follows symlinks) returned false, and the cleanup skipped them.
3. 33 minutes later the fresh `bin/k3s` made the old symlinks valid again —
   and the script's bare `ln -s k3s bin/k3s-agent` (no `-f`) died with
   `File exists`, after the main compile but **before** the containerd-shim
   and runc rebuilds, leaving a half-built tree.

The correct force-rebuild is to remove the binary *and* its sibling
symlinks:

```sh
find ~/k3s/bin -maxdepth 1 -type l -lname k3s -delete
rm -f ~/k3s/bin/k3s ~/k3s/dist/artifacts/k3s-riscv64
```

The re-run is much cheaper than the first pass — Go's build cache is keyed
on `GORISCV64`, so the 33-minute compile was not wasted: the retry re-links
from warm cache.

## Outcome

<!-- filled in after the rebuild completed -->

## Next

- Run `playbooks/11_riscv64_node_benchmark.yml` and compare the new row in
  [benchmarks.md](benchmarks.md) against the 20260711T111053 baseline
  (built with rva20u64). sysbench CPU is the column most likely to move;
  the honest expectation is low single-digit percent at best — Zba/Zbb help
  address math and byte-twiddling, not raw ALU loops.
- The `creates:`-guard force-rebuild procedure above is now the documented
  way to rebuild after any `k3s_build_env` change.
