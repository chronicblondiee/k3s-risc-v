# Third-party notices

## Remlab XSTIME assembler macros

The cgo kernel's `.insn r CUSTOM_1, funct3, 0x71, ...` mapping follows
Remlab's public `xstime.S` macro file:

- Source: https://www.remlab.net/op/xstime.S
- Copyright: Copyright (c) 2024 Remi Denis-Courmont. All rights reserved.
- License: MIT-style permission notice as published in that source file.

This tool does not vendor the macro file directly, but it uses the published
operand and `funct3` encoding map for the four `vmadot*` variants.
