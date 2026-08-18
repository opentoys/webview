#include "textflag.h"

// func blockInvoke(block uintptr) uintptr
// Reads the invoke pointer at byte offset 16 of an ObjC block struct.
TEXT ·blockInvoke(SB), NOSPLIT, $0-16
	MOVD block+0(FP), R0
	MOVD 16(R0), R0
	MOVD R0, ret+8(FP)
	RET
