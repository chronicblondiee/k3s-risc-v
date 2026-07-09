#!/bin/bash
# Builds the merged RK3588 DDR-init/SPL loader blob needed for
# `rkdeveloptool db <loader>` before most Maskrom recovery commands work.
#
# Rockchip's boot_merger tool (from the rkbin repo) is an x86_64 Linux ELF
# binary with no macOS build, so this runs it inside a Debian container via
# Docker Desktop (must be running - `open -a Docker` and wait if not). See
# ../docs/2026-07-07-nvme-install-brick-and-recovery.md for context.
set -euo pipefail

OUT_DIR="${1:-$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/rkbin}"

if [ ! -d "$OUT_DIR" ]; then
	git clone --depth 1 https://github.com/rockchip-linux/rkbin.git "$OUT_DIR"
fi

docker run --rm --platform linux/amd64 \
	-v "$OUT_DIR:/rkbin" \
	-w /rkbin \
	debian:bookworm \
	bash -c "chmod +x tools/boot_merger && ./tools/boot_merger RKBOOT/RK3588MINIALL.ini"

LOADER="$(find "$OUT_DIR" -maxdepth 1 -iname '*spl_loader*' | head -1)"
echo "Built: $LOADER"
