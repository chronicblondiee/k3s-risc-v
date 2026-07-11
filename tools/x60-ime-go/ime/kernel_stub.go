//go:build !(linux && riscv64 && cgo)

package ime

const kernelAvailable = false

func runKernel(dst *[16]int32, a, b *[32]byte, variant Variant) error {
	return ErrUnavailable
}
