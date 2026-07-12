//go:build !(linux && riscv64 && cgo)

package ime

const kernelAvailable = false

func runKernel(dst *[16]int32, a, b *[32]byte, variant Variant) error {
	return ErrUnavailable
}

func runMatrixKernel(dst []int32, a, b []byte, m, n, k int, variant Variant) error {
	return ErrUnavailable
}
