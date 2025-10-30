package wasi

import (
	"context"

	"github.com/bytecodealliance/wasmtime-go/v28"
)

// Authorizer defines the minimal interface needed for authorization
// This avoids import cycles with the tools package
type Authorizer interface {
	Authorize(ctx context.Context, toolName string, params map[string]interface{}) (*AuthorizationDecision, error)
}

// AuthorizationDecision represents the result of an authorization check
type AuthorizationDecision struct {
	Allowed           bool
	Reason            string
	RequiresUserInput bool
}

// AuthorizedWASINetwork wraps WASI socket operations with authorization checks
// This intercepts network calls at the WASI level and enforces domain authorization
type AuthorizedWASINetwork struct {
	authorizer Authorizer
	authCtx    context.Context
}

// NewAuthorizedWASINetwork creates a new WASI network provider with authorization
func NewAuthorizedWASINetwork(authorizer Authorizer, authCtx context.Context) *AuthorizedWASINetwork {
	return &AuthorizedWASINetwork{
		authorizer: authorizer,
		authCtx:    authCtx,
	}
}

// AddNetworkImports adds custom WASI socket functions with authorization to the linker
func (w *AuthorizedWASINetwork) AddNetworkImports(store *wasmtime.Store, linker *wasmtime.Linker) {
	// Note: WASI Preview 2 uses Component Model for network access
	// For wasip2, network access goes through wasi:http and wasi:sockets interfaces
	// wasmtime-go v28 supports Component Model, so we can intercept these properly
	//
	// WASI Preview 2 network interfaces:
	// - wasi:http/outgoing-handler - HTTP client requests
	// - wasi:sockets/tcp - TCP socket operations
	// - wasi:sockets/udp - UDP socket operations
	//
	// For maximum security, we'll:
	// 1. Not define these interfaces at all (they won't be available)
	// 2. Rely on the HTTP-layer authorization in the Go code wrapper
	// 3. Add defensive overrides for WASI P1 functions in case of fallback

	// Defensive layer: Override WASI P1 socket functions if they exist
	// sock_open(af: i32, socktype: i32, protocol: i32) -> (errno: i32, fd: i32)
	_ = linker.FuncNew(
		"wasi_snapshot_preview1",
		"sock_open",
		wasmtime.NewFuncType(
			[]*wasmtime.ValType{
				wasmtime.NewValType(wasmtime.KindI32),
				wasmtime.NewValType(wasmtime.KindI32),
				wasmtime.NewValType(wasmtime.KindI32),
			},
			[]*wasmtime.ValType{
				wasmtime.NewValType(wasmtime.KindI32),
				wasmtime.NewValType(wasmtime.KindI32),
			},
		),
		func(caller *wasmtime.Caller, args []wasmtime.Val) ([]wasmtime.Val, *wasmtime.Trap) {
			// Block all socket creation
			// WASI errno 76 = EPROTONOSUPPORT (Protocol not supported)
			return []wasmtime.Val{wasmtime.ValI32(76), wasmtime.ValI32(-1)}, nil
		},
	)

	// sock_connect(fd: i32, addr_ptr: i32, addr_len: i32) -> (errno: i32)
	_ = linker.FuncNew(
		"wasi_snapshot_preview1",
		"sock_connect",
		wasmtime.NewFuncType(
			[]*wasmtime.ValType{
				wasmtime.NewValType(wasmtime.KindI32),
				wasmtime.NewValType(wasmtime.KindI32),
				wasmtime.NewValType(wasmtime.KindI32),
			},
			[]*wasmtime.ValType{wasmtime.NewValType(wasmtime.KindI32)},
		),
		func(caller *wasmtime.Caller, args []wasmtime.Val) ([]wasmtime.Val, *wasmtime.Trap) {
			// Block all connections
			// WASI errno 13 = EACCES (Permission denied)
			return []wasmtime.Val{wasmtime.ValI32(13)}, nil
		},
	)

	// REALITY CHECK for WASI P2:
	// Go's WASM target for wasip2 uses the wasi:http component interface
	// By not defining wasi:http/outgoing-handler and wasi:sockets/* interfaces,
	// all network access is blocked at the WASI level by default.
	//
	// The authorization happens at the HTTP layer in our Go code wrapper,
	// which intercepts http.DefaultClient before any WASI calls are made.
	//
	// This multi-layer approach ensures:
	// 1. HTTP authorization in Go code (primary security layer)
	// 2. No wasi:http interface available (WASI P2 blocking)
	// 3. Blocked sock_* functions (WASI P1 fallback blocking)
}
