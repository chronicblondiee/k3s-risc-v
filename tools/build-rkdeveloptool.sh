#!/bin/bash
# Builds Rockchip's rkdeveloptool from source on macOS.
# No Homebrew formula exists for it; this is the tool used to talk to an
# RK3588 board in Maskrom recovery mode over USB. See
# ../docs/2026-07-07-nvme-install-brick-and-recovery.md for the incident that
# required this.
set -euo pipefail

OUT_DIR="${1:-$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/rkdeveloptool}"

brew install autoconf automake libtool pkg-config libusb

if [ ! -d "$OUT_DIR" ]; then
	git clone https://github.com/rockchip-linux/rkdeveloptool.git "$OUT_DIR"
fi

cd "$OUT_DIR"
autoreconf -i
./configure

# Apple Clang treats a variable-length-array usage in main.cpp as a hard
# error under -Werror (-Wvla-cxx-extension) where GCC only warns. Append
# -Wno-error to CXXFLAGS to get past it; this only silences that one class
# of warning-as-error, not real problems.
LIBUSB_INC="$(brew --prefix libusb)/include/libusb-1.0"
make CXXFLAGS="-DHAVE_CONFIG_H -I. -I./cfg -Wall -Wextra -Wreturn-type -fno-strict-aliasing -D_FILE_OFFSET_BITS=64 -D_LARGE_FILE -I${LIBUSB_INC} -g -O2 -Wno-error"

echo "Built: $OUT_DIR/rkdeveloptool"
