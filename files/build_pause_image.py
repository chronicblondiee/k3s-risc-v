#!/usr/bin/env python3
"""Build a minimal OCI image layout tarball wrapping a single static binary as /pause.

Runs on the target board so the binary and the packaging happen on the same
arch. Exists because no riscv64 build of any pause image (rancher/mirrored-pause,
registry.k8s.io/pause) is published anywhere upstream - see
docs/2026-07-09-k3s-riscv64-source-build.md for the version-by-version check.
"""
import hashlib
import io
import json
import os
import sys
import tarfile

PAUSE_BIN = sys.argv[1]
OUT_TAR = sys.argv[2]
IMAGE_REF = sys.argv[3]  # e.g. "localhost/pause:riscv64-local"

workdir = "/tmp/oci-pause-build"
os.system(f"rm -rf {workdir} && mkdir -p {workdir}/blobs/sha256")


def write_blob(data: bytes) -> tuple[str, int]:
    digest = hashlib.sha256(data).hexdigest()
    path = f"{workdir}/blobs/sha256/{digest}"
    with open(path, "wb") as f:
        f.write(data)
    return digest, len(data)


# --- layer: a tar containing /pause ---
layer_buf = io.BytesIO()
with tarfile.open(fileobj=layer_buf, mode="w") as tf:
    info = tarfile.TarInfo(name="pause")
    with open(PAUSE_BIN, "rb") as f:
        content = f.read()
    info.size = len(content)
    info.mode = 0o755
    info.mtime = 0
    tf.addfile(info, io.BytesIO(content))
layer_data = layer_buf.getvalue()
layer_digest, layer_size = write_blob(layer_data)

# --- image config ---
config = {
    "architecture": "riscv64",
    "os": "linux",
    "config": {
        "Entrypoint": ["/pause"],
    },
    "rootfs": {
        "type": "layers",
        "diff_ids": [f"sha256:{layer_digest}"],
    },
    "history": [{"created": "1970-01-01T00:00:00Z", "created_by": "manual riscv64 pause build"}],
}
config_bytes = json.dumps(config).encode()
config_digest, config_size = write_blob(config_bytes)

# --- manifest ---
manifest = {
    "schemaVersion": 2,
    "mediaType": "application/vnd.oci.image.manifest.v1+json",
    "config": {
        "mediaType": "application/vnd.oci.image.config.v1+json",
        "digest": f"sha256:{config_digest}",
        "size": config_size,
    },
    "layers": [
        {
            "mediaType": "application/vnd.oci.image.layer.v1.tar",
            "digest": f"sha256:{layer_digest}",
            "size": layer_size,
        }
    ],
}
manifest_bytes = json.dumps(manifest).encode()
manifest_digest, manifest_size = write_blob(manifest_bytes)

# --- index.json ---
index = {
    "schemaVersion": 2,
    "mediaType": "application/vnd.oci.image.index.v1+json",
    "manifests": [
        {
            "mediaType": "application/vnd.oci.image.manifest.v1+json",
            "digest": f"sha256:{manifest_digest}",
            "size": manifest_size,
            "platform": {"architecture": "riscv64", "os": "linux"},
            "annotations": {"org.opencontainers.image.ref.name": IMAGE_REF},
        }
    ],
}
with open(f"{workdir}/index.json", "w") as f:
    json.dump(index, f)

with open(f"{workdir}/oci-layout", "w") as f:
    json.dump({"imageLayoutVersion": "1.0.0"}, f)

# --- tar it all up ---
with tarfile.open(OUT_TAR, "w") as tf:
    tf.add(f"{workdir}/oci-layout", arcname="oci-layout")
    tf.add(f"{workdir}/index.json", arcname="index.json")
    tf.add(f"{workdir}/blobs", arcname="blobs")

print(f"Built {OUT_TAR} for {IMAGE_REF}")
