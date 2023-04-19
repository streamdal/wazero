package amd64

import (
	"encoding/hex"
	"testing"

	"github.com/tetratelabs/wazero/internal/asm"
	"github.com/tetratelabs/wazero/internal/testing/require"
)

func TestAssemblerImpl_CompileStaticConstToRegister(t *testing.T) {
	a := NewAssembler()
	t.Run("odd count of bytes", func(t *testing.T) {
		err := a.CompileStaticConstToRegister(MOVDQU, asm.NewStaticConst([]byte{1}), RegAX)
		require.Error(t, err)
	})
	t.Run("ok", func(t *testing.T) {
		cons := asm.NewStaticConst([]byte{1, 2, 3, 4})
		err := a.CompileStaticConstToRegister(MOVDQU, cons, RegAX)
		require.NoError(t, err)
		actualNode := a.current
		require.Equal(t, MOVDQU, actualNode.instruction)
		require.Equal(t, operandTypeStaticConst, actualNode.types.src)
		require.Equal(t, operandTypeRegister, actualNode.types.dst)
		require.Equal(t, cons, actualNode.staticConst)
	})
}

func TestAssemblerImpl_CompileRegisterToStaticConst(t *testing.T) {
	a := NewAssembler()
	t.Run("odd count of bytes", func(t *testing.T) {
		err := a.CompileRegisterToStaticConst(MOVDQU, RegAX, asm.NewStaticConst([]byte{1}))
		require.Error(t, err)
	})
	t.Run("ok", func(t *testing.T) {
		cons := asm.NewStaticConst([]byte{1, 2, 3, 4})
		err := a.CompileRegisterToStaticConst(MOVDQU, RegAX, cons)
		require.NoError(t, err)
		actualNode := a.current
		require.Equal(t, MOVDQU, actualNode.instruction)
		require.Equal(t, operandTypeRegister, actualNode.types.src)
		require.Equal(t, operandTypeStaticConst, actualNode.types.dst)
		require.Equal(t, cons, actualNode.staticConst)
	})
}

func TestAssemblerImpl_maybeFlushConstants(t *testing.T) {
	t.Run("no consts", func(t *testing.T) {
		a := NewAssembler()
		// Invoking maybeFlushConstants before encoding consts usage should not panic.
		a.maybeFlushConstants(false)
		a.maybeFlushConstants(true)
	})

	largeData := make([]byte, 256)

	tests := []struct {
		name                    string
		endOfFunction           bool
		dummyBodyBeforeFlush    []byte
		firstUseOffsetInBinary  uint64
		consts                  [][]byte
		expectedOffsetForConsts []uint64
		exp                     []byte
		maxDisplacement         int
	}{
		{
			name:                    "end of function",
			endOfFunction:           true,
			dummyBodyBeforeFlush:    []byte{'?', '?', '?', '?'},
			consts:                  [][]byte{{1, 2, 3, 4, 5, 6, 7, 8}, {10, 11, 12, 13}},
			expectedOffsetForConsts: []uint64{4, 4 + 8}, // 4 = len(dummyBodyBeforeFlush)
			firstUseOffsetInBinary:  0,
			exp:                     []byte{'?', '?', '?', '?', 1, 2, 3, 4, 5, 6, 7, 8, 10, 11, 12, 13},
			maxDisplacement:         1 << 31, // large displacement will emit the consts at the end of function.
		},
		{
			name:                   "not flush",
			endOfFunction:          false,
			dummyBodyBeforeFlush:   []byte{'?', '?', '?', '?'},
			consts:                 [][]byte{{1, 2, 3, 4, 5, 6, 7, 8}, {10, 11, 12, 13}},
			firstUseOffsetInBinary: 0,
			exp:                    []byte{'?', '?', '?', '?'},
			maxDisplacement:        1 << 31, // large displacement will emit the consts at the end of function.
		},
		{
			name:                    "not end of function but flush - short jump",
			endOfFunction:           false,
			dummyBodyBeforeFlush:    []byte{'?', '?', '?', '?'},
			consts:                  [][]byte{{1, 2, 3, 4, 5, 6, 7, 8}, {10, 11, 12, 13}},
			expectedOffsetForConsts: []uint64{4 + 2, 4 + 2 + 8}, // 4 = len(dummyBodyBeforeFlush), 2 = the size of jump
			firstUseOffsetInBinary:  0,
			exp: []byte{
				'?', '?', '?', '?',
				0xeb, 0x0c, // short jump with offset = len(consts[0]) + len(consts[1]) = 12 = 0xc.
				1, 2, 3, 4, 5, 6, 7, 8, 10, 11, 12, 13,
			},
			maxDisplacement: 0, // small displacement flushes the const immediately, not at the end of function.
		},
		{
			name:                    "not end of function but flush - long jump",
			endOfFunction:           false,
			dummyBodyBeforeFlush:    []byte{'?', '?', '?', '?'},
			consts:                  [][]byte{largeData},
			expectedOffsetForConsts: []uint64{4 + 5}, // 4 = len(dummyBodyBeforeFlush), 5 = the size of jump
			firstUseOffsetInBinary:  0,
			exp: append([]byte{
				'?', '?', '?', '?',
				0xe9, 0x0, 0x1, 0x0, 0x0, // short jump with offset = 256 = 0x0, 0x1, 0x0, 0x0 (in Little Endian).
			}, largeData...),
			maxDisplacement: 0, // small displacement flushes the const immediately, not at the end of function.
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			a := NewAssembler()
			a.MaxDisplacementForConstantPool = tc.maxDisplacement
			a.buf.Write(tc.dummyBodyBeforeFlush)

			for i, c := range tc.consts {
				sc := asm.NewStaticConst(c)
				a.pool.AddConst(sc, 100)
				i := i
				sc.AddOffsetFinalizedCallback(func(offsetOfConstInBinary uint64) {
					require.Equal(t, tc.expectedOffsetForConsts[i], offsetOfConstInBinary)
				})
			}

			a.pool.FirstUseOffsetInBinary = tc.firstUseOffsetInBinary
			a.maybeFlushConstants(tc.endOfFunction)

			require.Equal(t, tc.exp, a.buf.Bytes())
		})
	}
}

func TestAssemblerImpl_encodeRegisterToStaticConst(t *testing.T) {
	tests := []struct {
		name            string
		ins             asm.Instruction
		c               []byte
		reg             asm.Register
		ud2sBeforeConst int
		exp             []byte
	}{
		{
			name:            "cmp r12d, dword ptr [rip + 0x14]",
			ins:             CMPL,
			c:               []byte{0x1, 0x2, 0x3, 0x4, 0x5, 0x6, 0x7, 0x8},
			reg:             RegR12,
			ud2sBeforeConst: 10,
			exp: []byte{
				// cmp r12d, dword ptr [rip + 0x14]
				// where rip = 0x7, therefore [rip + 0x14] = [0x1b]
				0x44, 0x3b, 0x25, 0x14, 0x0, 0x0, 0x0,
				// UD2 * ud2sBeforeConst
				0xf, 0xb, 0xf, 0xb, 0xf, 0xb, 0xf, 0xb, 0xf, 0xb, 0xf, 0xb, 0xf, 0xb, 0xf, 0xb, 0xf, 0xb, 0xf, 0xb,
				// 0x1b: consts
				0x1, 0x2, 0x3, 0x4, 0x5, 0x6, 0x7, 0x8,
			},
		},
		{
			name:            "cmp eax, dword ptr [rip + 0x14]",
			ins:             CMPL,
			c:               []byte{0x1, 0x2, 0x3, 0x4, 0x5, 0x6, 0x7, 0x8},
			reg:             RegAX,
			ud2sBeforeConst: 10,
			exp: []byte{
				// cmp eax, dword ptr [rip + 0x14]
				// where rip = 0x6, therefore [rip + 0x14] = [0x1a]
				0x3b, 0x5, 0x14, 0x0, 0x0, 0x0,
				// UD2 * ud2sBeforeConst
				0xf, 0xb, 0xf, 0xb, 0xf, 0xb, 0xf, 0xb, 0xf, 0xb, 0xf, 0xb, 0xf, 0xb, 0xf, 0xb, 0xf, 0xb, 0xf, 0xb,
				// 0x1a: consts
				0x1, 0x2, 0x3, 0x4, 0x5, 0x6, 0x7, 0x8,
			},
		},
		{
			name:            "cmp r12, qword ptr [rip]",
			ins:             CMPQ,
			c:               []byte{0x1, 0x2, 0x3, 0x4, 0x5, 0x6, 0x7, 0x8},
			reg:             RegR12,
			ud2sBeforeConst: 0,
			exp: []byte{
				// cmp r12, qword ptr [rip]
				// where rip points to the end of this instruction == the const.
				0x4c, 0x3b, 0x25, 0x0, 0x0, 0x0, 0x0,
				0x1, 0x2, 0x3, 0x4, 0x5, 0x6, 0x7, 0x8,
			},
		},
		{
			name:            "cmp rsp, qword ptr [rip + 0xa]",
			ins:             CMPQ,
			c:               []byte{0x1, 0x2, 0x3, 0x4, 0x5, 0x6, 0x7, 0x8},
			reg:             RegSP,
			ud2sBeforeConst: 5,
			exp: []byte{
				// cmp rsp, qword ptr [rip + 0xa]
				// where rip = 0x6, therefore [rip + 0xa] = [0x11]
				0x48, 0x3b, 0x25, 0xa, 0x0, 0x0, 0x0,
				// UD2 * ud2sBeforeConst
				0xf, 0xb, 0xf, 0xb, 0xf, 0xb, 0xf, 0xb, 0xf, 0xb,
				// 0x11:
				0x1, 0x2, 0x3, 0x4, 0x5, 0x6, 0x7, 0x8,
			},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			a := NewAssembler()

			err := a.CompileRegisterToStaticConst(tc.ins, tc.reg, asm.NewStaticConst(tc.c))
			require.NoError(t, err)

			for i := 0; i < tc.ud2sBeforeConst; i++ {
				a.CompileStandAlone(UD2)
			}

			actual, err := a.Assemble()
			require.NoError(t, err)

			require.Equal(t, tc.exp, actual, hex.EncodeToString(actual))
		})
	}
}

func TestAssemblerImpl_encodeStaticConstToRegister(t *testing.T) {
	tests := []struct {
		name            string
		ins             asm.Instruction
		c               []byte
		reg             asm.Register
		ud2sBeforeConst int
		exp             []byte
	}{
		{
			name: "movdqu xmm14, xmmword ptr [rip + 0xa]",
			ins:  MOVDQU,
			c: []byte{
				0x1, 0x2, 0x3, 0x4, 0x5, 0x6, 0x7, 0x8,
				0x10, 0x20, 0x30, 0x40, 0x50, 0x60, 0x70, 0x80,
			},
			reg:             RegX14,
			ud2sBeforeConst: 5,
			exp: []byte{
				// movdqu xmm14, xmmword ptr [rip + 0xa]
				// where rip = 0x9, therefore [rip + 0xa] = [0x13]
				0xf3, 0x44, 0xf, 0x6f, 0x35, 0xa, 0x0, 0x0, 0x0,
				// UD2 * ud2sBeforeConst
				0xf, 0xb, 0xf, 0xb, 0xf, 0xb, 0xf, 0xb, 0xf, 0xb,
				// 0x13:
				0x1, 0x2, 0x3, 0x4, 0x5, 0x6, 0x7, 0x8,
				0x10, 0x20, 0x30, 0x40, 0x50, 0x60, 0x70, 0x80,
			},
		},
		{
			name: "movupd xmm1, xmmword ptr [rip + 0xa]",
			ins:  MOVUPD,
			c: []byte{
				0x1, 0x2, 0x3, 0x4, 0x5, 0x6, 0x7, 0x8,
				0x10, 0x20, 0x30, 0x40, 0x50, 0x60, 0x70, 0x80,
			},
			reg:             RegX1,
			ud2sBeforeConst: 5,
			exp: []byte{
				// movdqu xmm14, xmmword ptr [rip + 0xa]
				// where rip = 0x8, therefore [rip + 0xa] = [0x12]
				0x66, 0xf, 0x10, 0xd, 0xa, 0x0, 0x0, 0x0,
				// UD2 * ud2sBeforeConst
				0xf, 0xb, 0xf, 0xb, 0xf, 0xb, 0xf, 0xb, 0xf, 0xb,
				// 0x12:
				0x1, 0x2, 0x3, 0x4, 0x5, 0x6, 0x7, 0x8,
				0x10, 0x20, 0x30, 0x40, 0x50, 0x60, 0x70, 0x80,
			},
		},
		{
			name: "lea  r11, [rip + 0x14]",
			ins:  LEAQ,
			c: []byte{
				0x1, 0x2, 0x3, 0x4, 0x5, 0x6, 0x7, 0x8,
				0x10, 0x20, 0x30, 0x40, 0x50, 0x60, 0x70, 0x80,
			},
			reg:             RegR11,
			ud2sBeforeConst: 10,
			exp: []byte{
				// lea  r11, [rip + 0x14]
				// where rip = 0x7, therefore [rip + 0x14] = [0x1b]
				0x4c, 0x8d, 0x1d, 0x14, 0x0, 0x0, 0x0,
				// UD2 * ud2sBeforeConst
				0xf, 0xb, 0xf, 0xb, 0xf, 0xb, 0xf, 0xb, 0xf, 0xb,
				0xf, 0xb, 0xf, 0xb, 0xf, 0xb, 0xf, 0xb, 0xf, 0xb,
				// 0x1b:
				0x1, 0x2, 0x3, 0x4, 0x5, 0x6, 0x7, 0x8,
				0x10, 0x20, 0x30, 0x40, 0x50, 0x60, 0x70, 0x80,
			},
		},
		{
			name: "mov  r11d, dword ptr [rip + 0x3c]",
			ins:  MOVL,
			c: []byte{
				0x1, 0x2, 0x3, 0x4, 0x5, 0x6, 0x7, 0x8,
				0x10, 0x20, 0x30, 0x40, 0x50, 0x60, 0x70, 0x80,
			},
			reg:             RegR11,
			ud2sBeforeConst: 30,
			exp: []byte{
				// mov  r11d, dword ptr [rip + 0x3c]
				// where rip = 0x7, therefore [rip + 0x3c] = [0x43]
				0x44, 0x8b, 0x1d, 0x3c, 0x0, 0x0, 0x0,
				// UD2 * ud2sBeforeConst
				0xf, 0xb, 0xf, 0xb, 0xf, 0xb, 0xf, 0xb, 0xf, 0xb,
				0xf, 0xb, 0xf, 0xb, 0xf, 0xb, 0xf, 0xb, 0xf, 0xb,
				0xf, 0xb, 0xf, 0xb, 0xf, 0xb, 0xf, 0xb, 0xf, 0xb,
				0xf, 0xb, 0xf, 0xb, 0xf, 0xb, 0xf, 0xb, 0xf, 0xb,
				0xf, 0xb, 0xf, 0xb, 0xf, 0xb, 0xf, 0xb, 0xf, 0xb,
				0xf, 0xb, 0xf, 0xb, 0xf, 0xb, 0xf, 0xb, 0xf, 0xb,
				// 0x43:
				0x1, 0x2, 0x3, 0x4, 0x5, 0x6, 0x7, 0x8,
				0x10, 0x20, 0x30, 0x40, 0x50, 0x60, 0x70, 0x80,
			},
		},
		{
			name: "movd xmm14, dword ptr [rip + 0x3c]",
			ins:  MOVL,
			c: []byte{
				0x1, 0x2, 0x3, 0x4, 0x5, 0x6, 0x7, 0x8,
				0x10, 0x20, 0x30, 0x40, 0x50, 0x60, 0x70, 0x80,
			},
			reg:             RegX14,
			ud2sBeforeConst: 30,
			exp: []byte{
				// movd xmm14, dword ptr [rip + 0x3c]
				// where rip = 0x9, therefore [rip + 0x3c] = [0x45]
				0x66, 0x44, 0xf, 0x6e, 0x35, 0x3c, 0x0, 0x0, 0x0,
				// UD2 * ud2sBeforeConst
				0xf, 0xb, 0xf, 0xb, 0xf, 0xb, 0xf, 0xb, 0xf, 0xb,
				0xf, 0xb, 0xf, 0xb, 0xf, 0xb, 0xf, 0xb, 0xf, 0xb,
				0xf, 0xb, 0xf, 0xb, 0xf, 0xb, 0xf, 0xb, 0xf, 0xb,
				0xf, 0xb, 0xf, 0xb, 0xf, 0xb, 0xf, 0xb, 0xf, 0xb,
				0xf, 0xb, 0xf, 0xb, 0xf, 0xb, 0xf, 0xb, 0xf, 0xb,
				0xf, 0xb, 0xf, 0xb, 0xf, 0xb, 0xf, 0xb, 0xf, 0xb,
				// 0x45:
				0x1, 0x2, 0x3, 0x4, 0x5, 0x6, 0x7, 0x8,
				0x10, 0x20, 0x30, 0x40, 0x50, 0x60, 0x70, 0x80,
			},
		},
		{
			name: "mov  rsp, qword ptr [rip + 0x3c]",
			ins:  MOVQ,
			c: []byte{
				0x1, 0x2, 0x3, 0x4, 0x5, 0x6, 0x7, 0x8,
				0x10, 0x20, 0x30, 0x40, 0x50, 0x60, 0x70, 0x80,
			},
			reg:             RegSP,
			ud2sBeforeConst: 30,
			exp: []byte{
				// mov  rsp, qword ptr [rip + 0x3c]
				// where rip = 0x7, therefore [rip + 0x3c] = [0x43]
				0x48, 0x8b, 0x25, 0x3c, 0x0, 0x0, 0x0,
				// UD2 * ud2sBeforeConst
				0xf, 0xb, 0xf, 0xb, 0xf, 0xb, 0xf, 0xb, 0xf, 0xb,
				0xf, 0xb, 0xf, 0xb, 0xf, 0xb, 0xf, 0xb, 0xf, 0xb,
				0xf, 0xb, 0xf, 0xb, 0xf, 0xb, 0xf, 0xb, 0xf, 0xb,
				0xf, 0xb, 0xf, 0xb, 0xf, 0xb, 0xf, 0xb, 0xf, 0xb,
				0xf, 0xb, 0xf, 0xb, 0xf, 0xb, 0xf, 0xb, 0xf, 0xb,
				0xf, 0xb, 0xf, 0xb, 0xf, 0xb, 0xf, 0xb, 0xf, 0xb,
				// 0x43:
				0x1, 0x2, 0x3, 0x4, 0x5, 0x6, 0x7, 0x8,
				0x10, 0x20, 0x30, 0x40, 0x50, 0x60, 0x70, 0x80,
			},
		},
		{
			name: "movq xmm1, qword ptr [rip + 0x3c]",
			ins:  MOVQ,
			c: []byte{
				0x1, 0x2, 0x3, 0x4, 0x5, 0x6, 0x7, 0x8,
				0x10, 0x20, 0x30, 0x40, 0x50, 0x60, 0x70, 0x80,
			},
			reg:             RegX1,
			ud2sBeforeConst: 30,
			exp: []byte{
				// movq xmm1, qword ptr [rip + 0x3c]
				// where rip = 0x8, therefore [rip + 0x3c] = [0x44]
				0xf3, 0xf, 0x7e, 0xd, 0x3c, 0x0, 0x0, 0x0,
				// UD2 * ud2sBeforeConst
				0xf, 0xb, 0xf, 0xb, 0xf, 0xb, 0xf, 0xb, 0xf, 0xb,
				0xf, 0xb, 0xf, 0xb, 0xf, 0xb, 0xf, 0xb, 0xf, 0xb,
				0xf, 0xb, 0xf, 0xb, 0xf, 0xb, 0xf, 0xb, 0xf, 0xb,
				0xf, 0xb, 0xf, 0xb, 0xf, 0xb, 0xf, 0xb, 0xf, 0xb,
				0xf, 0xb, 0xf, 0xb, 0xf, 0xb, 0xf, 0xb, 0xf, 0xb,
				0xf, 0xb, 0xf, 0xb, 0xf, 0xb, 0xf, 0xb, 0xf, 0xb,
				// 0x44:
				0x1, 0x2, 0x3, 0x4, 0x5, 0x6, 0x7, 0x8,
				0x10, 0x20, 0x30, 0x40, 0x50, 0x60, 0x70, 0x80,
			},
		},
		{
			name:            "ucomisd xmm15, qword ptr [rip + 6]",
			ins:             UCOMISD,
			c:               []byte{0x1, 0x2, 0x3, 0x4, 0x5, 0x6, 0x7, 0x8},
			reg:             RegX15,
			ud2sBeforeConst: 3,
			exp: []byte{
				// ucomisd xmm15, qword ptr [rip + 6]
				// where rip = 0x9, therefore [rip + 6] = [0xf]
				0x66, 0x44, 0xf, 0x2e, 0x3d, 0x6, 0x0, 0x0, 0x0,
				// UD2 * ud2sBeforeConst
				0xf, 0xb, 0xf, 0xb, 0xf, 0xb,
				// 0xf:
				0x1, 0x2, 0x3, 0x4, 0x5, 0x6, 0x7, 0x8,
			},
		},
		{
			name:            "ucomiss xmm15, dword ptr [rip + 6]",
			ins:             UCOMISS,
			c:               []byte{0x1, 0x2, 0x3, 0x4, 0x5, 0x6, 0x7, 0x8},
			reg:             RegX15,
			ud2sBeforeConst: 3,
			exp: []byte{
				// ucomiss xmm15, dword ptr [rip + 6]
				// where rip = 0x8, therefore [rip + 6] = [0xe]
				0x44, 0xf, 0x2e, 0x3d, 0x6, 0x0, 0x0, 0x0,
				// UD2 * ud2sBeforeConst
				0xf, 0xb, 0xf, 0xb, 0xf, 0xb,
				// 0xe:
				0x1, 0x2, 0x3, 0x4, 0x5, 0x6, 0x7, 0x8,
			},
		},
		{
			name:            "subss xmm13, dword ptr [rip + 0xa]",
			ins:             SUBSS,
			c:               []byte{0x1, 0x2, 0x3, 0x4, 0x5, 0x6, 0x7, 0x8},
			reg:             RegX13,
			ud2sBeforeConst: 5,
			exp: []byte{
				// subss xmm13, dword ptr [rip + 0xa]
				// where rip = 0x9, therefore [rip + 0xa] = [0x13]
				0xf3, 0x44, 0xf, 0x5c, 0x2d, 0xa, 0x0, 0x0, 0x0,
				// UD2 * ud2sBeforeConst
				0xf, 0xb, 0xf, 0xb, 0xf, 0xb, 0xf, 0xb, 0xf, 0xb,
				// 0x12:
				0x1, 0x2, 0x3, 0x4, 0x5, 0x6, 0x7, 0x8,
			},
		},
		{
			name:            "subsd xmm1, qword ptr [rip + 0xa]",
			ins:             SUBSD,
			c:               []byte{0x1, 0x2, 0x3, 0x4, 0x5, 0x6, 0x7, 0x8},
			reg:             RegX1,
			ud2sBeforeConst: 5,
			exp: []byte{
				// subsd xmm1, qword ptr [rip + 0xa]
				// where rip = 0x8, therefore [rip + 0xa] = [0x12]
				0xf2, 0xf, 0x5c, 0xd, 0xa, 0x0, 0x0, 0x0,
				// UD2 * ud2sBeforeConst
				0xf, 0xb, 0xf, 0xb, 0xf, 0xb, 0xf, 0xb, 0xf, 0xb,
				// 0x12:
				0x1, 0x2, 0x3, 0x4, 0x5, 0x6, 0x7, 0x8,
			},
		},
		{
			name:            "cmp dword ptr [rip + 0x14], r12d",
			ins:             CMPL,
			c:               []byte{0x1, 0x2, 0x3, 0x4, 0x5, 0x6, 0x7, 0x8},
			reg:             RegR12,
			ud2sBeforeConst: 10,
			exp: []byte{
				// cmp dword ptr [rip + 0x14], r12d
				// where rip = 0x7, therefore [rip + 0x14] = [0x1b]
				0x44, 0x39, 0x25, 0x14, 0x0, 0x0, 0x0,
				// UD2 * ud2sBeforeConst
				0xf, 0xb, 0xf, 0xb, 0xf, 0xb, 0xf, 0xb, 0xf, 0xb, 0xf, 0xb, 0xf, 0xb, 0xf, 0xb, 0xf, 0xb, 0xf, 0xb,
				// 0x1b: consts
				0x1, 0x2, 0x3, 0x4, 0x5, 0x6, 0x7, 0x8,
			},
		},
		{
			name:            "cmp dword ptr [rip + 0x14], eax",
			ins:             CMPL,
			c:               []byte{0x1, 0x2, 0x3, 0x4, 0x5, 0x6, 0x7, 0x8},
			reg:             RegAX,
			ud2sBeforeConst: 10,
			exp: []byte{
				// cmp dword ptr [rip + 0x14], eax
				// where rip = 0x6, therefore [rip + 0x14] = [0x1a]
				0x39, 0x5, 0x14, 0x0, 0x0, 0x0,
				// UD2 * ud2sBeforeConst
				0xf, 0xb, 0xf, 0xb, 0xf, 0xb, 0xf, 0xb, 0xf, 0xb, 0xf, 0xb, 0xf, 0xb, 0xf, 0xb, 0xf, 0xb, 0xf, 0xb,
				// 0x1a: consts
				0x1, 0x2, 0x3, 0x4, 0x5, 0x6, 0x7, 0x8,
			},
		},
		{
			name:            "cmp qword ptr [rip], r12",
			ins:             CMPQ,
			c:               []byte{0x1, 0x2, 0x3, 0x4, 0x5, 0x6, 0x7, 0x8},
			reg:             RegR12,
			ud2sBeforeConst: 0,
			exp: []byte{
				// cmp qword ptr [rip], r12
				// where rip points to the end of this instruction == the const.
				0x4c, 0x39, 0x25, 0x0, 0x0, 0x0, 0x0,
				0x1, 0x2, 0x3, 0x4, 0x5, 0x6, 0x7, 0x8,
			},
		},
		{
			name:            "cmp qword ptr [rip + 0xa], rsp",
			ins:             CMPQ,
			c:               []byte{0x1, 0x2, 0x3, 0x4, 0x5, 0x6, 0x7, 0x8},
			reg:             RegSP,
			ud2sBeforeConst: 5,
			exp: []byte{
				// cmp qword ptr [rip + 0xa], rsp
				// where rip = 0x6, therefore [rip + 0xa] = [0x11]
				0x48, 0x39, 0x25, 0xa, 0x0, 0x0, 0x0,
				// UD2 * ud2sBeforeConst
				0xf, 0xb, 0xf, 0xb, 0xf, 0xb, 0xf, 0xb, 0xf, 0xb,
				// 0x11:
				0x1, 0x2, 0x3, 0x4, 0x5, 0x6, 0x7, 0x8,
			},
		},
		{
			name:            "ucomiss xmm15, dword ptr [rip + 6]",
			ins:             UCOMISS,
			c:               []byte{0x1, 0x2, 0x3, 0x4, 0x5, 0x6, 0x7, 0x8},
			reg:             RegX15,
			ud2sBeforeConst: 3,
			exp: []byte{
				// ucomiss xmm15, dword ptr [rip + 6]
				// where rip = 0x8, therefore [rip + 6] = [0xe]
				0x44, 0xf, 0x2e, 0x3d, 0x6, 0x0, 0x0, 0x0,
				// UD2 * ud2sBeforeConst
				0xf, 0xb, 0xf, 0xb, 0xf, 0xb,
				// 0xe:
				0x1, 0x2, 0x3, 0x4, 0x5, 0x6, 0x7, 0x8,
			},
		},
		{
			name:            "subss xmm13, dword ptr [rip + 0xa]",
			ins:             SUBSS,
			c:               []byte{0x1, 0x2, 0x3, 0x4, 0x5, 0x6, 0x7, 0x8},
			reg:             RegX13,
			ud2sBeforeConst: 5,
			exp: []byte{
				// subss xmm13, dword ptr [rip + 0xa]
				// where rip = 0x9, therefore [rip + 0xa] = [0x13]
				0xf3, 0x44, 0xf, 0x5c, 0x2d, 0xa, 0x0, 0x0, 0x0,
				// UD2 * ud2sBeforeConst
				0xf, 0xb, 0xf, 0xb, 0xf, 0xb, 0xf, 0xb, 0xf, 0xb,
				// 0x12:
				0x1, 0x2, 0x3, 0x4, 0x5, 0x6, 0x7, 0x8,
			},
		},
		{
			name:            "subsd xmm1, qword ptr [rip + 0xa]",
			ins:             SUBSD,
			c:               []byte{0x1, 0x2, 0x3, 0x4, 0x5, 0x6, 0x7, 0x8},
			reg:             RegX1,
			ud2sBeforeConst: 5,
			exp: []byte{
				// subsd xmm1, qword ptr [rip + 0xa]
				// where rip = 0x8, therefore [rip + 0xa] = [0x12]
				0xf2, 0xf, 0x5c, 0xd, 0xa, 0x0, 0x0, 0x0,
				// UD2 * ud2sBeforeConst
				0xf, 0xb, 0xf, 0xb, 0xf, 0xb, 0xf, 0xb, 0xf, 0xb,
				// 0x12:
				0x1, 0x2, 0x3, 0x4, 0x5, 0x6, 0x7, 0x8,
			},
		},
		{
			name:            "add eax, dword ptr [rip + 0xa]",
			ins:             ADDL,
			c:               []byte{0x1, 0x2, 0x3, 0x4, 0x5, 0x6, 0x7, 0x8},
			reg:             RegAX,
			ud2sBeforeConst: 5,
			exp: []byte{
				// add eax, dword ptr [rip + 0xa]
				// where rip = 0x6, therefore [rip + 0xa] = [0x10]
				0x3, 0x5, 0xa, 0x0, 0x0, 0x0,
				// UD2 * ud2sBeforeConst
				0xf, 0xb, 0xf, 0xb, 0xf, 0xb, 0xf, 0xb, 0xf, 0xb,
				// 0x10:
				0x1, 0x2, 0x3, 0x4, 0x5, 0x6, 0x7, 0x8,
			},
		},
		{
			name:            "add rax, qword ptr [rip + 0xa]",
			ins:             ADDQ,
			c:               []byte{0x1, 0x2, 0x3, 0x4, 0x5, 0x6, 0x7, 0x8},
			reg:             RegAX,
			ud2sBeforeConst: 5,
			exp: []byte{
				// add rax, dword ptr [rip + 0xa]
				// where rip = 0x7, therefore [rip + 0xa] = [0x11]
				0x48, 0x3, 0x5, 0xa, 0x0, 0x0, 0x0,
				// UD2 * ud2sBeforeConst
				0xf, 0xb, 0xf, 0xb, 0xf, 0xb, 0xf, 0xb, 0xf, 0xb,
				// 0x11:
				0x1, 0x2, 0x3, 0x4, 0x5, 0x6, 0x7, 0x8,
			},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			a := NewAssembler()

			err := a.CompileStaticConstToRegister(tc.ins, asm.NewStaticConst(tc.c), tc.reg)
			require.NoError(t, err)

			for i := 0; i < tc.ud2sBeforeConst; i++ {
				a.CompileStandAlone(UD2)
			}

			actual, err := a.Assemble()
			require.NoError(t, err)

			require.Equal(t, tc.exp, actual, hex.EncodeToString(actual))
		})
	}
}
