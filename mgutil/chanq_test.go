package mgutil

import (
	"fmt"
	"testing"
)

func TestCtxQ(t *testing.T) {
	for _, i := range []int{0, -1} {
		name := fmt.Sprintf("NewCtxQ(%d)", i)
		t.Run(name, func(t *testing.T) {
			defer func() {
				if v := recover(); v == nil {
					t.Errorf("%s does not result in a panic", name)
				}
			}()
			NewCtxQ(i)
		})
	}

	cq := NewCtxQ(1)
	lastVal := -1
	for i := 0; i < 3; i++ {
		lastVal = i
		cq.Put(lastVal)
	}
	if v := <-cq.C(); v != lastVal {
		t.Error("CtxQ.Put does not appear to clear the old value")
	}
}
