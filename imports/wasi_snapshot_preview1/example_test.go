package wasi_snapshot_preview1_test

import (
	"context"
	_ "embed"
	"fmt"
	"os"

	"github.com/streamdal/wazero"
	"github.com/streamdal/wazero/imports/wasi_snapshot_preview1"
	"github.com/streamdal/wazero/sys"
)

// exitOnStartWasm was generated by the following:
//
//	cd testdata; wat2wasm --debug-names exit_on_start.wat
//
//go:embed testdata/exit_on_start.wasm
var exitOnStartWasm []byte

// This is an example of how to use WebAssembly System Interface (WASI) with its simplest function: "proc_exit".
//
// See https://github.com/tetratelabs/wazero/tree/main/examples/wasi for another example.
func Example() {
	// Choose the context to use for function calls.
	ctx := context.Background()

	// Create a new WebAssembly Runtime.
	r := wazero.NewRuntime(ctx)
	defer r.Close(ctx)

	// Instantiate WASI, which implements system I/O such as console output.
	wasi_snapshot_preview1.MustInstantiate(ctx, r)

	// InstantiateModule runs the "_start" function which is like a "main" function.
	mod, err := r.InstantiateWithConfig(ctx, exitOnStartWasm,
		// Override default configuration (which discards stdout).
		wazero.NewModuleConfig().WithStdout(os.Stdout).WithName("wasi-demo"))
	if mod != nil {
		defer r.Close(ctx)
	}

	// Note: Most compilers do not exit the module after running "_start", unless
	// there was an error. This allows you to call exported functions.
	if exitErr, ok := err.(*sys.ExitError); ok {
		fmt.Printf("exit_code: %d\n", exitErr.ExitCode())
	}

	// Output:
	// exit_code: 2
}
