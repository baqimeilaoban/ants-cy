package ants_cy

import (
	"fmt"
	"testing"
	"time"
)

func TestAntsPool(t *testing.T) {
	p, err := NewTimingPool(10, 2)
	defer p.Release()
	if err != nil {
		fmt.Printf("err is: %v\n", err)
	}
	for i := 0; i < 100; i++ {
		i := i
		if err := p.Submit(func() {
			time.Sleep(1 * time.Second)
			fmt.Printf("hello word: %d\n", i)
		}); err != nil {
			fmt.Printf("submit failed: %v\n", err)
		}
	}

}
