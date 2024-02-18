package ants_cy

import (
	"runtime"
	"sync"
	"sync/atomic"
)

type spinLock uint32

// Lock 加锁
func (sl *spinLock) Lock() {
	for !atomic.CompareAndSwapUint32((*uint32)(sl), 0, 1) {
		runtime.Gosched()
	}
}

// Unlock 解锁
func (sl *spinLock) Unlock() {
	atomic.StoreUint32((*uint32)(sl), 0)
}

// SpinLock 生成锁实例
func SpinLock() sync.Locker {
	return new(spinLock)
}
