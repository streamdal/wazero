package frontend

import (
	"github.com/streamdal/wazero/internal/engine/wazevo/ssa"
	"github.com/streamdal/wazero/internal/wasm"
)

func FunctionIndexToFuncRef(idx wasm.Index) ssa.FuncRef {
	return ssa.FuncRef(idx)
}
