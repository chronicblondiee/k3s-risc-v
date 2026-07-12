# SpacemiT llama.cpp HTTP sidecar

**Status:** validated on `k8s-rv2-01` on 2026-07-12. The playbook completed
successfully with the deployment `Running`, the PVC `Bound`, `/health`
returning `{"status":"ok"}`, and `/v1/chat/completions` returning a real chat
completion from the Qwen 2.5 0.5B smoke model.

## Intent

Run SpacemiT's riscv64 `llama.cpp` runtime as a Kubernetes-hosted HTTP
inference service, instead of calling IME instructions from Go. Go clients use
the OpenAI-compatible llama.cpp server API over ClusterIP:

```text
http://spacemit-llama-cpp.riscv64-llm.svc.cluster.local:8080/v1/chat/completions
```

This keeps cgo, `unsafe`, and vendor matrix-instruction details out of the Go
inference path. `tools/x60-ime-go` remains a proof/benchmark harness.

## Artifacts

- Playbook:
  [`playbooks/13_riscv64_llama_cpp_sidecar.yml`](../playbooks/13_riscv64_llama_cpp_sidecar.yml)
- Manifest template:
  [`templates/riscv64-llama-cpp.yaml.j2`](../templates/riscv64-llama-cpp.yaml.j2)
- Runtime artifact:
  `https://archive.spacemit.com/spacemit-ai/llama.cpp/spacemit-llama.cpp.riscv64.0.0.9.tar.gz`
- Runtime ONNX dependency:
  `https://archive.spacemit.com/spacemit-ai/onnxruntime/spacemit-ort.riscv64.2.0.2.tar.gz`
- Smoke model:
  `https://archive.spacemit.com/spacemit-ai/model_zoo/llm/qwen2.5-0.5b-instruct-q4_0.gguf`

## Deployment

Prerequisites:

- `playbooks/05_k3s_riscv64_build.yml` has installed the standalone k3s
  server.
- `playbooks/06_riscv64_registry.yml` has deployed the local registry and
  written `/etc/rancher/k3s/registries.yaml` for plain-HTTP pulls.
- The node is `Ready`.

Run:

```bash
ansible-playbook playbooks/13_riscv64_llama_cpp_sidecar.yml --limit k8s-rv2-01
```

The playbook checks whether
`http://<node>:30500/v2/spacemit-llama-cpp/tags/list` already includes
`riscv64-local`. If not, it installs `buildah`, `netavark`, `crun`, and
`nftables`, builds the image on the node, fails during the image build if no
`llama-server` or `server` executable is present in the SpacemiT tarball,
validates that the server's dynamic dependencies resolve, and pushes the image
to:

```text
<node>:30500/spacemit-llama-cpp:riscv64-local
```

The Kubernetes side creates:

- namespace `riscv64-llm`
- PVC `llama-models` using `local-path`, default size `8Gi`
- deployment `spacemit-llama-cpp`, one replica, `Recreate`
- ClusterIP service `spacemit-llama-cpp:8080`

The init container downloads the Qwen 2.5 0.5B GGUF into the PVC if it is missing.
The serving container sets `SPACEMIT_MEM_BACKEND=POSIX` because the default
SpacemiT `HPAGE` backend failed on this board without `/dev/tcm_sync_mem` or a
hugepage device.

If the image tag already exists but the image definition changed, run with:

```bash
ansible-playbook playbooks/13_riscv64_llama_cpp_sidecar.yml --limit k8s-rv2-01 -e llama_cpp_force_rebuild=true
```

## Validation

The playbook waits for the PVC to bind, waits for rollout, then runs an
in-cluster smoke Job using the same image:

- `GET http://spacemit-llama-cpp:8080/health`
- `POST http://spacemit-llama-cpp:8080/v1/chat/completions` with a tiny prompt
  and `max_tokens: 8`

At the end it prints the pod, service, and PVC state plus the first lines of
the smoke response. There is intentionally no LAN NodePort for inference in
this iteration.

Validated smoke response excerpt:

```json
{"status":"ok"}
{"choices":[{"finish_reason":"stop","index":0,"message":{"role":"assistant","content":"Ok!"}}],"model":"qwen2.5-0.5b-instruct-q4_0.gguf"}
```

## Discovery And Fixes

This section records the debugging path because most failures were specific
to SpacemiT's prebuilt runtime, not ordinary Kubernetes plumbing.

### Runtime Archive Shape

The first image build treated the SpacemiT llama.cpp tarball as opaque and
failed fast unless it contained an executable named `llama-server` or
`server`. The tarball layout was:

```text
spacemit-llama.cpp.riscv64.0.0.9/bin/llama-server
spacemit-llama.cpp.riscv64.0.0.9/lib/*.so*
```

The entrypoint therefore cannot assume the binary sits at `/opt/.../llama-server`;
it searches under `/opt/spacemit-llama-cpp` and exports all discovered
`/opt/**/lib` directories in `LD_LIBRARY_PATH` before execing the server.

### Smoke Model URL

The original planned smoke model URL returned 404:

```text
https://archive.spacemit.com/spacemit-ai/ModelZoo/gguf/qwen2.5-0.5b-q4_0_16_8.gguf
```

The public archive has both old `ModelZoo/` and newer `model_zoo/` trees. The
available Qwen 2.5 0.5B GGUF lives in the newer tree:

```text
https://archive.spacemit.com/spacemit-ai/model_zoo/llm/qwen2.5-0.5b-instruct-q4_0.gguf
```

The init container crash-looped until this was corrected. The fixed init
container completed and left the model on the `llama-models` PVC.

### Dynamic Libraries

After the model URL was fixed, the serving container exited immediately with
exit code `127`. A one-shot debug pod from the image showed `llama-server`
was present, but `ldd` reported unresolved libraries:

```text
libatomic.so.1 => not found
libllama-common.so.0 => not found
libmtmd.so.0 => not found
libllama.so.0 => not found
libggml.so.0 => not found
libggml-cpu.so.0 => not found
libggml-base.so.0 => not found
libonnxruntime.so.1 => not found
```

Fixes:

- install `libatomic1` and `libgomp1` in the image
- add SpacemiT's extracted `lib/` directories to `LD_LIBRARY_PATH`
- validate `ldd` during image build so missing libraries fail before deploy

### ONNX Runtime Version

Debian trixie has `libonnxruntime1.21`, but SpacemiT's `llama-server`
requires a newer symbol version:

```text
libonnxruntime.so.1: version `VERS_1.24.2' not found
```

The matching runtime is published separately in SpacemiT's archive:

```text
https://archive.spacemit.com/spacemit-ai/onnxruntime/spacemit-ort.riscv64.2.0.2.tar.gz
```

That tarball includes:

```text
lib/libonnxruntime.so.1
lib/libonnxruntime.so.1.24.2+spacemit.a1
```

The image now downloads and extracts this artifact under `/opt/spacemit-ort`
instead of using Debian's ONNX runtime package.

### Stable Tag Refresh

The image tag is intentionally stable:

```text
spacemit-llama-cpp:riscv64-local
```

During troubleshooting, rebuilding and pushing the same tag did not change the
Deployment spec, so Kubernetes kept reusing an older local image. The template
now sets `imagePullPolicy: Always` for the init container, serving container,
and smoke job. The playbook also runs `rollout restart` when
`llama_cpp_build_required` is true, so `-e llama_cpp_force_rebuild=true`
refreshes a same-tag image predictably.

### SpacemiT Memory Backend

With libraries resolved, the server started and loaded model metadata but then
failed tensor allocation:

```text
CPU_RISCV64_SPACEMIT: alloc_chunk: open(/dev/tcm_sync_mem) failed, errno=2
CPU_RISCV64_SPACEMIT: alloc_chunk: madvise(MADV_HUGEPAGE) failed
alloc_tensor_range: failed to allocate CPU_RISCV64_SPACEMIT buffer
```

The host also lacks `/dev/tcm_sync_mem` and `/dev/hugepages`, so mounting a
device into the pod was not enough. Searching the SpacemiT `libggml-cpu.so`
binary exposed the vendor knob and supported values:

```text
SPACEMIT_MEM_BACKEND
none
posix
hpage
hpage1gb
```

The deployment sets:

```text
SPACEMIT_MEM_BACKEND=POSIX
```

This avoids the failed hugepage path while still using SpacemiT's riscv64
runtime. With that environment variable set, the deployment rolled out and the
HTTP smoke job passed.
