package wasi

import (
	"context"
	"sync"

	"github.com/bytecodealliance/wasmtime-go/v28"
)

// HostState provides shared state between host functions and WASM instance
// This allows host functions to access the instance's memory after instantiation
type HostState struct {
	mu         sync.RWMutex
	memory     *wasmtime.Memory
	store      *wasmtime.Store
	authorizer Authorizer
	authCtx    context.Context
}

// NewHostState creates a new host state
func NewHostState(authorizer Authorizer, authCtx context.Context) *HostState {
	return &HostState{
		authorizer: authorizer,
		authCtx:    authCtx,
	}
}

// SetStore sets the WASM store (needed for memory access)
func (h *HostState) SetStore(store *wasmtime.Store) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.store = store
}

// SetMemory sets the WASM instance memory (called after instantiation)
func (h *HostState) SetMemory(memory *wasmtime.Memory) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.memory = memory
}

// GetMemory gets the WASM instance memory
func (h *HostState) GetMemory() *wasmtime.Memory {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.memory
}

// GetStore gets the WASM store
func (h *HostState) GetStore() *wasmtime.Store {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.store
}

// AddAuthorizationImports adds authorization host functions that use the shared state
func (h *HostState) AddAuthorizationImports(store *wasmtime.Store, linker *wasmtime.Linker) {
	h.SetStore(store)

	// authorize_domain(domain_ptr: i32, domain_len: i32) -> i32
	_ = linker.FuncNew(
		"env",
		"authorize_domain",
		wasmtime.NewFuncType(
			[]*wasmtime.ValType{wasmtime.NewValType(wasmtime.KindI32), wasmtime.NewValType(wasmtime.KindI32)},
			[]*wasmtime.ValType{wasmtime.NewValType(wasmtime.KindI32)},
		),
		func(caller *wasmtime.Caller, args []wasmtime.Val) ([]wasmtime.Val, *wasmtime.Trap) {
			domainPtr := args[0].I32()
			domainLen := args[1].I32()

			// Get memory from the caller context
			memory := caller.GetExport("memory").Memory()
			if memory == nil {
				// Memory not available, deny
				return []wasmtime.Val{wasmtime.ValI32(0)}, nil
			}

			// Read domain string from WASM memory
			data := memory.UnsafeData(caller)
			if domainPtr < 0 || domainLen < 0 || int(domainPtr)+int(domainLen) > len(data) {
				// Invalid memory access, deny
				return []wasmtime.Val{wasmtime.ValI32(0)}, nil
			}

			domain := string(data[domainPtr : domainPtr+domainLen])

			// Check authorization
			if h.authorizer != nil {
				decision, err := h.authorizer.Authorize(h.authCtx, "go_sandbox_domain", map[string]interface{}{
					"domain": domain,
				})

				if err == nil && decision != nil && decision.Allowed {
					return []wasmtime.Val{wasmtime.ValI32(1)}, nil
				}
			}

			// Not authorized
			return []wasmtime.Val{wasmtime.ValI32(0)}, nil
		},
	)
}
