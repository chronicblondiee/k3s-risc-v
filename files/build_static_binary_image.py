#!/usr/bin/env python3
"""Build a minimal OCI image layout tarball wrapping a single static binary.

Generalized from build_pause_image.py (which stays as-is, dedicated to the
pause image) for reuse anywhere else a CGO_ENABLED=0 Go binary has no
riscv64 build published upstream (e.g. metrics-server) - same reasoning:
see docs/2026-07-09-k3s-riscv64-source-build.md for the version-by-version
check that established this pattern.
"""
import hashlib
import io
import json
import os
import sys
import tarfile

BINARY_PATH = sys.argv[1]
IN_IMAGE_PATH = sys.argv[2]  # e.g. "/metrics-server" - path+entrypoint inside the image
OUT_TAR = sys.argv[3]
IMAGE_REF = sys.argv[4]  # e.g. "localhost/metrics-server:riscv64-local"
ARCH = sys.argv[5] if len(sys.argv) > 5 else "riscv64"

workdir = "/tmp/oci-single-binary-build"
os.system(f"rm -rf {workdir} && mkdir -p {workdir}/blobs/sha256")


def write_blob(data: bytes) -> tuple[str, int]:
    digest = hashlib.sha256(data).hexdigest()
    path = f"{workdir}/blobs/sha256/{digest}"
    with open(path, "wb") as f:
        f.write(data)
    return digest, len(data)


# --- layer: a tar containing the binary at IN_IMAGE_PATH ---
in_image_name = IN_IMAGE_PATH.lstrip("/")
layer_buf = io.BytesIO()
with tarfile.open(fileobj=layer_buf, mode="w") as tf:
    info = tarfile.TarInfo(name=in_image_name)
    with open(BINARY_PATH, "rb") as f:
        content = f.read()
    info.size = len(content)
    info.mode = 0o755
    info.mtime = 0
    tf.addfile(info, io.BytesIO(content))
layer_data = layer_buf.getvalue()
layer_digest, layer_size = write_blob(layer_data)

# --- image config ---
config = {
    "architecture": ARCH,
    "os": "linux",
    "config": {
        "Entrypoint": [IN_IMAGE_PATH],
    },
    "rootfs": {
        "type": "layers",
        "diff_ids": [f"sha256:{layer_digest}"],
    },
    "history": [{"created": "1970-01-01T00:00:00Z", "created_by": "manual riscv64 single-binary image build"}],
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
            "platform": {"architecture": ARCH, "os": "linux"},
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
