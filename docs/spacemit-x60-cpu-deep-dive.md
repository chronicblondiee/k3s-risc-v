# SpacemiT X60 CPU deep dive (Orange Pi RV2 / Ky X1)

**Board:** Orange Pi RV2 (`k8s-rv2-01`)
**SoC:** Ky X1 — a rebranded SpacemiT K1 (same silicon as the Banana Pi BPI-F3)
**Cores:** 8x SpacemiT X60, riscv64, 1.6 GHz, no SMT
**Kernel at time of writing:** 6.18.35-current-spacemit (Armbian, Debian 13)

Companion to [benchmarks.md](benchmarks.md): that page tracks *how fast* the
node is across k3s builds; this one records *what the core actually is* and
which of its features are worth leveraging. Every hardware claim below was
either read off the node itself (command shown) or comes from a cited source.

## The core in one paragraph

The X60 is an in-order, dual-issue, 8-stage RV64GC core implementing (almost —
see [the honesty section](#where-it-falls-short)) the RVA22 profile **plus**
the ratified RVV 1.0 vector extension at VLEN=256. Its microarchitecture is
widely described as a XuanTie C908 class design with the vector datapath
doubled (2x128-bit units). The K1 packages eight of them as two clusters of
four, each cluster sharing a 512 KB L2 — there is no L3. It also hides a
proprietary AI trick: SpacemiT's IME matrix instructions (the marketing
"2 TOPS AI CPU" claim) live inside these cores, not in a separate NPU.

## Verified on the node

### ISA string

```
$ cat /proc/cpuinfo   # (one hart shown; all 8 identical)
isa: rv64imafdcv_zicbom_zicboz_zicntr_zicond_zicsr_zifencei_zihintpause_zihpm
     _zaamo_zalrsc_zfh_zfhmin_zca_zcd_zba_zbb_zbc_zbs_zkt
     _zve32f_zve32x_zve64d_zve64f_zve64x_zvfh_zvfhmin_zvkt
     _sscofpmf_sstc_svinval_svnapot_svpbmt
mmu: sv39
uarch: spacemit,x60
mvendorid: 0x710        # SpacemiT
```

### Vector length — measured, not assumed

Tiny probe compiled on-node (`gcc 14.2, -march=rv64gcv`) reading the `vlenb`
CSR:

```c
unsigned long vlenb;
__asm__ volatile("csrr %0, vlenb" : "=r"(vlenb));
```

Output: **`VLENB=32 bytes VLEN=256 bits`** — the datasheet's VLEN=256 claim is
real. Userspace vector is enabled by default
(`/proc/sys/abi/riscv_v_default_allow` = 1), so any binary compiled with
`-march=rv64gcv` runs vector code with no opt-in.

### Topology and caches

```
$ cat /sys/devices/system/cpu/cpu0/cache/index*/{level,type,size,shared_cpu_list}
L1I 32K  per core          (lscpu's "256 KiB (8 instances)" = 8 x 32K)
L1D 32K  per core
L2  512K shared by cpu0-3  (second 512K instance shared by cpu4-7)
```

Two 4-core clusters, each with its own 512 KB L2, no L3. Cross-cluster data
sharing goes through DRAM-side coherency, not a shared cache. cpufreq:
614 MHz – 1.6 GHz, governor already `performance` on this node.

### What the kernel probed at boot

```
$ dmesg | grep -iE 'riscv|misaligned'
riscv-timer: Timer interrupt in S-mode is available via sstc extension
cpu0-7: ... unaligned accesses are fast          (hardware, ~2x byte-access cost)
riscv-pmu-sbi: SBI PMU extension is available
riscv-pmu-sbi: 16 firmware and 18 hardware counters
```

Three quiet wins already active with zero configuration: `sstc` (timer
interrupts handled directly in S-mode instead of trapping to firmware —
relevant on a timer-heavy Go/Kubernetes stack), fast **scalar** unaligned
access in hardware, and a working PMU.

## Extension inventory, annotated

| Group | Extensions | What it buys us |
|---|---|---|
| Base | `rv64imafdc` + `zca/zcd` (compressed) | Standard RV64GC — what the current k3s binary targets |
| Vector | `v` (RVV 1.0, VLEN=256) + `zve32x/32f/64x/64f/64d` | Full 256-bit vector unit incl. FP64; the single biggest untapped feature |
| Half-precision FP | `zfh`, `zfhmin`, `zvfh`, `zvfhmin` | Native FP16 scalar **and** vector — ML inference without FP32 blowup |
| Bit manipulation | `zba` (address gen), `zbb` (basic), `zbc` (carry-less mul), `zbs` (single-bit) | Faster array indexing, popcount/rotate/min-max, CRC-style polynomial math |
| Conditional ops | `zicond` | Branchless select — helps an in-order core that pays full price for mispredicts |
| Cache ops | `zicbom` (management), `zicboz` (cache-line zero) | Kernel uses `cbo.zero` for fast page zeroing automatically |
| Constant-time | `zkt`, `zvkt` | Data-independent execution latency guarantee (scalar + vector) — crypto *safety*, not crypto *speed* (see below) |
| Counters/hints | `zicntr`, `zihpm`, `zihintpause` | Cycle/instret counters, spin-loop hint |
| Atomics | `zaamo`, `zalrsc` | Standard A split — nothing exotic |
| Supervisor | `sstc`, `sscofpmf`, `svinval`, `svnapot`, `svpbmt` | S-mode timers, PMU sampling/filtering, finer TLB invalidation, 64K NAPOT pages, page-based memory types |
| Vendor (hidden) | XSpacemiT IME (`vmadot` family) | 16 proprietary matrix-multiply/sliding-window instructions reusing the vector registers — the "2 TOPS" AI claim. Not in the ISA string, not in mainline toolchains |

## What we can leverage

Ordered roughly by effort-to-payoff for this repo.

### 1. Rebuild k3s with `GORISCV64=rva22u64` (near-free)

`playbooks/05_k3s_riscv64_build.yml` currently sets only `GOARCH: riscv64`,
so Go targets the **rva20u64** baseline and ignores every extension above.
Since Go 1.23 the compiler takes
[`GORISCV64`](https://github.com/golang/go/issues/61476): at `rva22u64` it
emits `zba`/`zbb` instructions (sh*add address math, min/max, rotates,
sign-extension) throughout the runtime and stdlib — all present on the X60.
One env line in playbook 05, then a before/after with playbook 11 to see if
the CPU columns in [benchmarks.md](benchmarks.md) move.

**Do not** use `GORISCV64=rva23u64`: RVA23 assumes `zfa`, `zawrs`, `zimop`
and vector-crypto extensions the X60 does not have — a binary built for it
can hit illegal-instruction faults on this core.

### 2. Native builds: the right `-march`, and a toolchain caveat

For C/C++ we compile on-device (buildah images, the pause image):

- The node's gcc 14.2 **rejects** `-mcpu=spacemit-x60` (verified); safe flags
  today are `-march=rv64gcv_zba_zbb_zbc_zbs_zicond_zfh -mabi=lp64d`.
- Clang/LLVM ≥ 18 knows
  [`-mcpu=spacemit-x60`](https://lists.llvm.org/pipermail/cfe-commits/Week-of-Mon-20240603/585553.html),
  and the X60's in-order scheduling model is now good enough that LLVM 23
  [adopted it as the `-mtune=generic` default](https://github.com/llvm/llvm-project/commit/5e8d4064bc74)
  for RISC-V. On an in-order core, scheduling quality matters far more than
  on the out-of-order chips we're used to — a newer compiler is itself a
  performance feature here.
- **Caveat:** GCC 14 autovectorization can emit *misaligned vector* loads,
  which this core traps on (see below). If a vectorized native build
  mysteriously SIGBUS/SIGILLs, suspect this first; GCC 15 reportedly fixed
  the codegen.

### 3. The vector unit is real — use it for data-plane work

VLEN=256 with linear LMUL scaling and solid throughput on ordinary
arithmetic ([measured on this exact core](https://camel-cdr.github.io/rvv-bench-results/spacemit_x60/index.html)).
Best fits on a k8s node: memcpy/memset-heavy paths, checksums, compression,
JSON/UTF-8 scanning — anything streaming. Two caveats from the same
measurements: `vrgather.vv` and `vcompress.vm` are very slow at high LMUL, so
shuffle/table-lookup-based SIMD algorithms (some UTF-8/base64 tricks) don't
carry over well.

### 4. FP16 + IME: on-node ML inference is plausible

- `zvfh` gives *vector* half-precision — llama.cpp's RISC-V path can use RVV,
  and FP16 halves memory bandwidth needs, which is the real limit on this
  board (~7-14 GB/s, see benchmarks.md).
- The proprietary [IME `vmadot` instructions](https://github.com/spacemit-com/riscv-ime-extension-spec)
  do INT8 matrix-multiply-accumulate in the vector registers; SpacemiT's
  [llama.cpp/Ollama fork demonstrates ~2 TOPS-class speedups](https://www.bit-brick.com/2024/12/04/k1-ai-cpu-deployment-of-large-models-based-on-llama-cpp-and-ollama/)
  on the K1. Mainline toolchains don't emit these — using them means
  SpacemiT's Bianbu forks or hand-rolled `.insn` encodings. Interesting
  weekend experiment, not infrastructure. Concrete implementation options
  (including from Go) are written up in
  [spacemit-x60-ime-from-go.md](spacemit-x60-ime-from-go.md).

### 5. Profiling actually works

`sscofpmf` + the SBI PMU (18 hardware counters) means `perf record` /
`perf stat` sampling works properly on this node — many RISC-V boards can't
say that. When a k3s build regresses in benchmarks.md, we can profile the
node instead of guessing.

### 6. Cluster-aware pinning

Two independent 512 KB L2 domains (cpu0-3 / cpu4-7) and no L3 means
cache-hot workloads sharing data want to stay inside one cluster, and two
noisy pods are better placed in *different* clusters. Kubelet's static CPU
manager policy (`--cpu-manager-policy=static` + Guaranteed pods with integer
CPU requests) or plain `taskset` for host processes can exploit this. Only
worth it if a latency-sensitive workload ever lands on this node.

## Where it falls short

Being honest about the same silicon:

- **Not quite RVA22:** the profile mandates misaligned access support
  (`Zicclsm`) for scalar *and vector*; the X60 does scalar in hardware but
  [traps on misaligned vector access, and neither kernel nor OpenSBI emulate
  it](https://community.milkv.io/t/spacemit-k1-m1-is-not-quite-rva22-compliant/2870).
  Binaries built "for RVA22 with V" by an unaware compiler can crash. This is
  the sharpest edge on the whole chip.
- **No crypto instructions.** `zkt`/`zvkt` are only *timing* guarantees — the
  scalar (`zkn*`/`zks*`) and vector (`zvkn*`) crypto extensions are absent, so
  OpenSSL (3.5.6 on the node) runs AES/SHA as portable C. Every TLS handshake
  and etcd/API-server byte on this k3s node pays scalar-software crypto
  prices. Nothing to fix; just budget for it.
- **No hypervisor extension (`h`).** KVM is impossible — containers-only on
  this board is physics, not a choice.
- **Sv39 MMU:** 39-bit virtual addressing (256 GB user VA). Irrelevant at
  8 GB RAM, but rules out big-VA tricks (sanitizers are happier on Sv48).
- **In-order, 1.6 GHz, no L3** — single-thread sysbench ~791 ev/s
  (benchmarks.md) is Raspberry Pi 3-class scalar performance. The
  interesting compute on this chip is in the vector unit, not the scalar
  pipeline.
- No `zicbop` (prefetch hints), no `zfa`, no `zawrs` — minor, but they're why
  RVA23-targeted binaries are unsafe here.

## Candidate experiments (in repo terms)

1. Add `GORISCV64: rva22u64` to playbook 05's build environment, rebuild k3s,
   run playbook 11, compare rows in benchmarks.md. Cheapest possible win.
2. `openssl speed -evp aes-256-gcm sha256` baseline now, so a future distro
   OpenSSL with RVV-aware C (or a kernel with crypto offload) shows up as a
   diff.
3. Build llama.cpp with `-march=rv64gcv_zfh_zvfh` and a small quantized model
   as an on-node inference smoke test; optionally compare SpacemiT's IME fork.
4. `perf stat` a benchmark-11 run to get baseline IPC/cache-miss numbers for
   the node while it's healthy.

## Sources

- Node evidence: `/proc/cpuinfo`, `/sys/devices/system/cpu/*/cache/*`,
  `dmesg`, `vlenb` CSR probe — captured 2026-07-11 on `k8s-rv2-01`.
- [RT-RK: GCC tuning for SpacemiT X60 (in-order dual-issue scheduler model)](https://www.rt-rk.com/gcc-tuning-for-spacemit-x60-building-an-in-order-dual-issue-scheduler-model-part-i/)
- [camel-cdr RVV benchmark results on SpacemiT X60](https://camel-cdr.github.io/rvv-bench-results/spacemit_x60/index.html)
- [Banana Pi docs: SpacemiT K1 brief](https://docs.banana-pi.org/en/BPI-F3/SpacemiT_K1)
- [CNX Software: Orange Pi RV2 / Ky X1 announcement](https://www.cnx-software.com/2025/03/08/orange-pi-rv2-low-cost-risc-v-sbc-ky-x1-octa-core-soc-2-tops-ai-accelerator/)
- [Milk-V forum: SpacemiT K1/M1 is not quite RVA22 compliant](https://community.milkv.io/t/spacemit-k1-m1-is-not-quite-rva22-compliant/2870)
- [golang/go#61476: GORISCV64 environment variable](https://github.com/golang/go/issues/61476)
- [LLVM: SpacemiT-X60 processor definition (PR #94564)](https://lists.llvm.org/pipermail/cfe-commits/Week-of-Mon-20240603/585553.html) and [X60 model as `-mtune=generic`](https://github.com/llvm/llvm-project/commit/5e8d4064bc74)
- [SpacemiT IME extension spec (vmadot)](https://github.com/spacemit-com/riscv-ime-extension-spec)
- [BIT-BRICK: K1 llama.cpp/Ollama deployment with AI instructions](https://www.bit-brick.com/2024/12/04/k1-ai-cpu-deployment-of-large-models-based-on-llama-cpp-and-ollama/)
