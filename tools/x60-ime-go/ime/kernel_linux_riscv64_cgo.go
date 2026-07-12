//go:build linux && riscv64 && cgo

package ime

/*
#cgo CFLAGS: -march=rv64gcv
#include <stdint.h>

// The vmadot encodings follow Remlab's xstime.S macro map:
// .insn r CUSTOM_1, funct3, 0x71, x4, x1, x2
// funct3: 3=signed/signed, 0=unsigned/unsigned, 2=signed/unsigned,
// 1=unsigned/signed. Keep the copyright/license note for that reference
// with this source tree's documentation.
static void x60_ime_vmadot_tile(int32_t *dst, const uint8_t *a, const uint8_t *b, int variant) {
	__asm__ volatile(
		"vsetivli zero, 16, e32, m2, ta, ma\n\t"
		"vle32.v v4, (%[dst])\n\t"
		"li t0, 32\n\t"
		"vsetvli zero, t0, e8, m1, ta, ma\n\t"
		"vle8.v v1, (%[a])\n\t"
		"vle8.v v2, (%[b])\n\t"
		"li t0, 1\n\t"
		"beq %[variant], t0, 1f\n\t"
		"li t0, 2\n\t"
		"beq %[variant], t0, 2f\n\t"
		"li t0, 3\n\t"
		"beq %[variant], t0, 3f\n\t"
		".insn r CUSTOM_1, 3, 0x71, x4, x1, x2\n\t"
		"j 4f\n\t"
		"1:\n\t"
		".insn r CUSTOM_1, 0, 0x71, x4, x1, x2\n\t"
		"j 4f\n\t"
		"2:\n\t"
		".insn r CUSTOM_1, 2, 0x71, x4, x1, x2\n\t"
		"j 4f\n\t"
		"3:\n\t"
		".insn r CUSTOM_1, 1, 0x71, x4, x1, x2\n\t"
		"4:\n\t"
		"vsetivli zero, 16, e32, m2, ta, ma\n\t"
		"vse32.v v4, (%[dst])\n\t"
		:
		: [dst] "r"(dst), [a] "r"(a), [b] "r"(b), [variant] "r"(variant)
		: "memory", "t0", "v1", "v2", "v4", "v5");
}

static void x60_ime_vmadot_matrix(
	int32_t *dst,
	const uint8_t *a,
	const uint8_t *b,
	int m,
	int n,
	int k,
	int variant
) {
	uint8_t atile[32];
	uint8_t btile[32];
	int32_t dtile[16];

	for (int row = 0; row < m; row += 4) {
		for (int col = 0; col < n; col += 4) {
			for (int tr = 0; tr < 4; tr++) {
				for (int tc = 0; tc < 4; tc++) {
					dtile[tr * 4 + tc] = dst[(row + tr) * n + col + tc];
				}
			}
			for (int kk = 0; kk < k; kk += 8) {
				for (int tr = 0; tr < 4; tr++) {
					for (int tk = 0; tk < 8; tk++) {
						atile[tr * 8 + tk] = a[(row + tr) * k + kk + tk];
					}
				}
				for (int tc = 0; tc < 4; tc++) {
					for (int tk = 0; tk < 8; tk++) {
						btile[tc * 8 + tk] = b[(col + tc) * k + kk + tk];
					}
				}
				x60_ime_vmadot_tile(dtile, atile, btile, variant);
			}
			for (int tr = 0; tr < 4; tr++) {
				for (int tc = 0; tc < 4; tc++) {
					dst[(row + tr) * n + col + tc] = dtile[tr * 4 + tc];
				}
			}
		}
	}
}
*/
import "C"

const kernelAvailable = true

func runKernel(dst *[16]int32, a, b *[32]byte, variant Variant) error {
	C.x60_ime_vmadot_tile(
		(*C.int32_t)(&dst[0]),
		(*C.uint8_t)(&a[0]),
		(*C.uint8_t)(&b[0]),
		C.int(variant),
	)
	return nil
}

func runMatrixKernel(dst []int32, a, b []byte, m, n, k int, variant Variant) error {
	C.x60_ime_vmadot_matrix(
		(*C.int32_t)(&dst[0]),
		(*C.uint8_t)(&a[0]),
		(*C.uint8_t)(&b[0]),
		C.int(m),
		C.int(n),
		C.int(k),
		C.int(variant),
	)
	return nil
}
