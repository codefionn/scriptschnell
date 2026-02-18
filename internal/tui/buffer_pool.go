package tui

import (
	"strings"
	"sync"

	"github.com/codefionn/scriptschnell/internal/consts"
)

const maxBuilderCapacity = consts.BufferSize256KB

var builderPool = sync.Pool{
	New: func() any {
		return new(strings.Builder)
	},
}

func acquireBuilder() *strings.Builder {
	b := builderPool.Get().(*strings.Builder)
	b.Reset()
	return b
}

func releaseBuilder(b *strings.Builder) {
	if b == nil {
		return
	}
	if b.Cap() > maxBuilderCapacity {
		return
	}
	b.Reset()
	builderPool.Put(b)
}

func builderString(b *strings.Builder) string {
	if b == nil {
		return ""
	}
	s := strings.Clone(b.String())
	releaseBuilder(b)
	return s
}
