#include "textflag.h"

// func callBlockASM(block, arg uintptr)
// Reads the invoke function pointer from the block struct at offset 16 and
// calls it with (block, arg) — matching the ObjC block calling convention
// where the first argument is the block pointer itself.
TEXT ·callBlockASM(SB), NOSPLIT, $0-16
	MOVQ block+0(FP), DI
	MOVQ 16(DI), AX
	MOVQ arg+8(FP), SI
	JMP AX
