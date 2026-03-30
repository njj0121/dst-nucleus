#include "textflag.h"


// func testcputime() uint64
TEXT ·testcputime(SB), NOSPLIT, $0-8
    LFENCE 
    RDTSC
    SHLQ $32, DX
    ORQ  DX, AX
    MOVQ AX, ret+0(FP) 
    RET

// func getCPUBaseFreq() uint32
TEXT ·getCPUBaseFreq(SB), NOSPLIT, $0-4
    MOVL $0, AX
    CPUID
    CMPL AX, $0x16
    JL   not_supported
    MOVL $0x16, AX
    CPUID
    MOVL AX, ret+0(FP) 
    RET

not_supported:
    MOVL $0, ret+0(FP)
    RET
