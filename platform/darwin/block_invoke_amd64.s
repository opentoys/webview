#include "textflag.h"

// func blockInvoke(block uintptr) uintptr
// Reads the invoke pointer at byte offset 16 of an ObjC block struct.
TEXT ·blockInvoke(SB), NOSPLIT, $0-16
	MOVQ block+0(FP), AX
	MOVQ 16(AX), AX
	MOVQ AX, ret+8(FP)
	RET
