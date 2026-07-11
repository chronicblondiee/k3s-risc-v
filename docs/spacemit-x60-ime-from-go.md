# Using the X60's IME matrix instructions from Go (future work)

**Status:** not implemented — this documents the viable approaches so we can
revisit later. Companion to
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

## Recommendation and revisit triggers

Default plan when we pick this up: **Approach B first** (cheapest path to a
working `vmadot`), benchmarked against a plain-RVV and a pure-Go INT8 GEMM
of the same shape; promote to Approach A only if cgo ergonomics annoy, or to
C/D only if a real inference workload lands on the node. Worth revisiting
if any of these change:

- Go gains vendor-extension or intrinsics support (unlikely; watch
  [golang/go#61476](https://github.com/golang/go/issues/61476) descendants).
- IME (or the competing ratified matrix extension "AME"/attached-matrix work
  upstream) gets standardized — the spec repo positions IME as a proposal
  under a RISC-V IME standard track.
- Mainline GCC/LLVM grow XSTIME support (would upgrade Approach B to plain
  intrinsics).

## Verification plan (when implemented)

1. SIGILL smoke test: run the kernel under the `/proc/cpuinfo` gate on the
   node; confirm a clean fallback path off-node (CI runners are not X60s).
2. Correctness: compare `vmadot` GEMM output against a pure-Go int32
   reference for random int8 matrices, including the signedness variants.
3. Performance: microbenchmark scalar vs RVV vs IME INT8 GEMM at a few sizes;
   record results in [benchmarks.md](benchmarks.md) style — the interesting
   number is how close we get to the claimed ~2 TOPS.

## Sources

- [SpacemiT IME extension spec (vmadot family)](https://github.com/spacemit-com/riscv-ime-extension-spec)
- [Remlab: SpacemiT Integrated Matrix Extension (XSTIME) — semantics, CUSTOM_1 encoding, xstime.S macros](https://www.remlab.net/op/riscv-xstime.shtml)
- [Go riscv64 assembler (RVV mnemonics)](https://pkg.go.dev/cmd/internal/obj/riscv)
- [GORISCV64 proposal — scope limited to ratified profiles](https://github.com/golang/go/issues/61476)
- [BIT-BRICK: K1 llama.cpp/Ollama deployment using the AI instructions](https://www.bit-brick.com/2024/12/04/k1-ai-cpu-deployment-of-large-models-based-on-llama-cpp-and-ollama/)
