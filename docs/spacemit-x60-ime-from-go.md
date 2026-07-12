# Using the X60's IME matrix instructions from Go

**Status:** prototype benchmark implemented in
[`tools/x60-ime-go`](../tools/x60-ime-go). Latest fetched node result:
[`benchmarks/results/k8s-rv2-01-ime-20260711T124020.md`](../benchmarks/results/k8s-rv2-01-ime-20260711T124020.md).
Companion to
[spacemit-x60-cpu-deep-dive.md](spacemit-x60-cpu-deep-dive.md), which covers
what IME is in the context of the whole core.

## The 30-second recap

SpacemiT's Integrated Matrix Extension (IME, vendor extension "XSTIME") is a
set of proprietary matrix-multiply instructions baked into the X60 cores of
this node's Ky X1/K1 SoC — it's where the "2 TOPS AI CPU" marketing number
comes from. Key facts from the [spec](https://github.com/spacemit-com/riscv-ime-extension-spec)
and [Remlab's analysis](https://www.remlab.net/op/riscv-xstime.shtml):

- Four `vmadot` variants by signedness (`vmadot`, `vmadotu`, `vmadotsu`,
  `vmadotus`): widening INT8 matrix multiply-accumulate,
  `MVD += wide(MVS1) * transpose(wide(MVS2))`.
- Tiles: sources are 4x4 or 4x8 of int8 held in vector registers;
  destination is 4x4 of int32. Destination register number must be even
  (EMUL=2).
- Reuses the RVV register file and CSRs — no new architectural state. Set up
  with ordinary `vsetivli` (SEW=8, LMUL=1; `vl` = 16 or 32 on our VLEN=256
  core), then issue the matrix ops.
- Encoded in the **CUSTOM_1 opcode space** — this is why no ratified profile
  will ever include it and no mainline compiler will ever emit it.
- Not in the ISA string, not detectable via hwprobe. The only runtime signal
  we have is `/proc/cpuinfo` (`uarch: spacemit,x60`, `mvendorid: 0x710`).

Go's compiler is therefore out of the picture permanently (`GORISCV64` only
targets ratified profiles). The options below are ordered from most to least
"native Go".

## Approach A — pure Go assembly, hand-encoded instructions

Go ships a full riscv64 assembler with
[RVV mnemonics as of the Go we already build k3s with](https://pkg.go.dev/cmd/internal/obj/riscv)
(`VSETVLI`/`VSETIVLI`, `VLE8V`, `VSE32V`, registers `V0`-`V31`). Everything
around the matrix op can be written as real assembly; only the `vmadot`
instructions themselves need raw encodings via `WORD`:

```asm
// func vmadotKernel(dst *int32, a, b *int8)  — SKETCH, encodings TBD from spec
TEXT ·vmadotKernel(SB), NOSPLIT, $0
    MOV  dst+0(FP), X10
    MOV  a+8(FP), X11
    MOV  b+16(FP), X12
    VSETIVLI $16, E8, M1, TA, MA, X0   // SEW=8, LMUL=1, vl=16
    VLE8V (X11), V1                     // load tile A
    VLE8V (X12), V2                     // load tile B
    WORD  $0x00000000                   // vmadot v4, v1, v2 — derive encoding
                                        // from spec (CUSTOM_1, vd even)
    // ... vsetvli for e32 result, VSE32V V4 -> dst ...
    RET
```

Wrap it in a package with a build tag and a runtime gate:

- `//go:build riscv64` + an `init()` that parses `/proc/cpuinfo` for
  `uarch\s*:\s*spacemit,x60` (or mvendorid 0x710) and sets `HasIME bool`;
  fall back to a pure-Go (or plain-RVV) implementation otherwise.
- Goroutine safety is free: the kernel (6.18 here) saves/restores vector
  state across context switches and signals, so Go's scheduler migration and
  async preemption can't corrupt tiles mid-kernel, same as any RVV code.

**Cost:** deriving and hand-verifying the CUSTOM_1 encodings (the spec +
Remlab's `xstime.S` macro file are the references); a SIGILL test harness is
mandatory. **Benefit:** zero cgo, single static binary, fits the k3s/Go
toolchain we already have on the node.

## Approach B — cgo + `.insn` inline asm, stock toolchain

The node's stock Debian gcc 14 can emit arbitrary CUSTOM_1 instructions with
the `.insn` directive without understanding them — Remlab's
[`xstime.S`](https://www.remlab.net/op/riscv-xstime.shtml) does exactly this
via assembler macros. A ~100-line C file (vsetivli + vle8 + `.insn`-encoded
vmadot + vse32) exposed as `int8_gemm_tile(...)`, called through cgo:

- No fork toolchains, builds on-device with what playbook 05 already
  installs.
- cgo overhead (~tens of ns/call) is irrelevant if the C side processes whole
  matrices, not single tiles — design the boundary accordingly.
- Same `/proc/cpuinfo` gating as Approach A, done on the Go side.

This is the pragmatic middle: real assembler macros instead of hand-hexed
`WORD`s, still no vendor toolchain.

**Implemented prototype:** `tools/x60-ime-go` is a standalone Go module with
an `ime` package and `cmd/imebench` CLI. It implements only the fixed X60
VLEN=256 shape used for the first proof:

- `A`: 4x8 INT8, row-major.
- `B`: 4x8 INT8, row-major, consumed as transposed by `vmadot*`.
- `dst`: 4x4 INT32 accumulator, row-major.
- Operation: `dst += widen(A) * transpose(widen(B))`.

The Linux/riscv64+cgo path uses RVV loads/stores plus Remlab's confirmed
CUSTOM_1 encoding pattern:

```asm
.insn r CUSTOM_1, funct3, 0x71, x4, x1, x2
```

where `v4/v5` hold the even destination register group, `v1` is `A`, and
`v2` is `B`. `funct3` is `3` for signed/signed, `0` for unsigned/unsigned,
`2` for signed/unsigned, and `1` for unsigned/signed. The cgo file sets
`-march=rv64gcv` explicitly because GCC's default target flags on this node
do not enable RVV mnemonics for inline assembler.

`imebench selftest` runs the actual IME instruction first in a child process,
so a bad encoding fails as a controlled child `SIGILL` instead of killing the
parent process. It then compares all four signedness variants against the
pure-Go reference over deterministic edge cases and randomized inputs.

`playbooks/12_riscv64_ime_go_benchmark.yml` copies the module to the node,
requires the existing `~/sdk/go/bin/go` and `gcc` from the k3s source-build
setup, runs `go test ./...`, `imebench selftest`, and `imebench bench`, then
fetches Markdown and JSON reports to `benchmarks/results/`. Do not add these
IME numbers to `docs/benchmarks.md`; keep them separate from the general node
benchmark history.

First successful run on 2026-07-11:

- CPU gate: `uarch: spacemit,x60`, `mvendorid: 0x710`.
- `go test ./...`: passed on-node with `CGO_ENABLED=1`.
- SIGILL child probe and correctness selftest: passed.
- Tiny per-tile benchmark, 200,000 iterations per variant: pure Go measured
  about 1.4-1.75 us/tile; IME measured about 360-366 ns/tile. This is useful
  proof of execution, not a tuned throughput number, because it still crosses
  cgo once per tile.

Follow-up local changes extend the proof harness from single-tile calls to
matrix-shaped calls that loop over 4x4x8 IME tiles:

- `MulMatrix` / `AccumulateMatrix` validate `m % 4 == 0`, `n % 4 == 0`, and
  `k % 8 == 0`, clear or preserve `dst` consistently with the existing tile
  APIs, and return `ErrInvalidShape` for bad dimensions.
- `ReferenceMulMatrix` / `ReferenceAccumulateMatrix` provide the pure-Go
  comparison path for the same signedness variants.
- The riscv64+cgo kernel copies each 4x8 `A` tile and each matching 4x8 `B`
  tile into fixed tile buffers, invokes the proven `vmadot` tile kernel, and
  writes each 4x4 accumulator block back to the row-major destination.
- `imebench selftest` now checks both tile correctness and matrix correctness.
- `imebench bench` now reports dimensions, MACs/op, ns/op, ns/tile,
  tiles/sec, and int8 MAC/s for the original 4x4x8 tile path plus larger
  matrix shapes. The matrix path is still a proof harness, not a production
  GEMM: it deliberately prioritizes correctness and transparent tiling over
  avoiding temporary copies.

## Approach C — cgo against SpacemiT's own kernels

SpacemiT's Bianbu toolchain forks understand IME natively, and their
[llama.cpp/Ollama work](https://www.bit-brick.com/2024/12/04/k1-ai-cpu-deployment-of-large-models-based-on-llama-cpp-and-ollama/)
already contains tuned INT8 GEMM kernels. Link those (or a library built with
their gcc fork) via cgo. Most performance for least novel code, but drags a
vendor toolchain into the build and their forks track upstream loosely.
Only worth it if Approach B's naive kernel measurably underperforms.

## Approach D — don't put it in Go at all (sidecar)

For the one realistic workload (quantized LLM inference): build SpacemiT's
llama.cpp fork as a riscv64 container image on-device (same buildah + local
registry flow playbooks 06/07/11 use — build once, push to
`<node>:30500`), run it as a deployment, and let Go talk to it over HTTP.
Zero unsafe code in Go, plays to the repo's existing image-build machinery,
and the IME weirdness stays quarantined in one image.

**Recommended path for real LLM inference:** use
[`playbooks/13_riscv64_llama_cpp_sidecar.yml`](../playbooks/13_riscv64_llama_cpp_sidecar.yml).
It packages SpacemiT's pinned riscv64 llama.cpp runtime artifact into the
local registry as `spacemit-llama-cpp:riscv64-local`, deploys it in the
`riscv64-llm` namespace, downloads the Qwen 2.5 0.5B smoke GGUF onto a
`local-path` PVC, and exposes only an internal ClusterIP service:

```text
http://spacemit-llama-cpp.riscv64-llm.svc.cluster.local:8080/v1/chat/completions
```

Go callers should use that OpenAI-compatible HTTP API. Do not add cgo,
`unsafe`, or IME-specific code to ordinary Go inference clients.

## Recommendation and revisit triggers

Default path is now **Approach D** for quantized LLM inference:
`playbooks/13_riscv64_llama_cpp_sidecar.yml` keeps vendor IME behavior inside
SpacemiT's runtime image and gives Go code a plain HTTP boundary. Keep
**Approach B** via `tools/x60-ime-go` as an instruction-level proof and
benchmark harness only; it should not become the production inference path.
Promote Approach A only if there is a concrete need for native Go IME kernels
outside LLM serving, or Approach C only if the sidecar cannot meet an actual
workload target. Worth revisiting if any of these change:

- Go gains vendor-extension or intrinsics support (unlikely; watch
  [golang/go#61476](https://github.com/golang/go/issues/61476) descendants).
- IME (or the competing ratified matrix extension "AME"/attached-matrix work
  upstream) gets standardized — the spec repo positions IME as a proposal
  under a RISC-V IME standard track.
- Mainline GCC/LLVM grow XSTIME support (would upgrade Approach B to plain
  intrinsics).

## Verification plan

1. Local/off-node: `go test ./...` in `tools/x60-ime-go`; `imebench detect`
   should report no IME without crashing.
2. On `k8s-rv2-01`: for the IME proof harness, run
   `ansible-playbook playbooks/12_riscv64_ime_go_benchmark.yml --limit k8s-rv2-01`.
3. Confirm the fetched Markdown and JSON report exist under
   `benchmarks/results/`.
4. Treat the current cgo-per-tile benchmark as proof of instruction execution
   and correctness only. A real throughput study should move larger matrix
   loops across the cgo boundary and add a plain-RVV comparison.

For the recommended llama.cpp sidecar path, run:

```bash
ansible-playbook playbooks/13_riscv64_llama_cpp_sidecar.yml --limit k8s-rv2-01
```

The playbook performs its own registry check, Kubernetes rollout wait, and
in-cluster `/health` plus `/v1/chat/completions` smoke job. The validated
runbook is
[`docs/2026-07-12-riscv64-llama-cpp-sidecar.md`](2026-07-12-riscv64-llama-cpp-sidecar.md).

## Sources

- [SpacemiT IME extension spec (vmadot family)](https://github.com/spacemit-com/riscv-ime-extension-spec)
- [Remlab: SpacemiT Integrated Matrix Extension (XSTIME) — semantics, CUSTOM_1 encoding, xstime.S macros](https://www.remlab.net/op/riscv-xstime.shtml)
- [Go riscv64 assembler (RVV mnemonics)](https://pkg.go.dev/cmd/internal/obj/riscv)
- [GORISCV64 proposal — scope limited to ratified profiles](https://github.com/golang/go/issues/61476)
- [BIT-BRICK: K1 llama.cpp/Ollama deployment using the AI instructions](https://www.bit-brick.com/2024/12/04/k1-ai-cpu-deployment-of-large-models-based-on-llama-cpp-and-ollama/)
