package ants_cy

import (
	"errors"
	"math"
	"runtime"
)

const (
	DEFAULT_ANTS_POOL_SIZE     = math.MaxInt32
	DEFAULT_CLEAN_INERVAL_TIME = 1 // 默认清理线程的间隔时间
	CLOSE                      = 1 // 表明协程池是否关闭
)

var (
	ErrInvalidPoolSize   = errors.New("协程数量无效")
	ErrInvalidPoolExpiry = errors.New("线程无效清理时间")
	ErrPoolClosed        = errors.New("协程池已被关闭")
	ErrPoolOverload      = errors.New("协程池已过载")
)

var (
	workChanCap = func() int {
		// 判断是否可以并行执行多任务
		if runtime.GOMAXPROCS(0) == 1 {
			return 0
		}
		return 1
	}
	// 引入包时直接初始化一个
	defaultAntsPool, _ = NewPool(DEFAULT_ANTS_POOL_SIZE)
)

// Submit 提交一个任务到协程池
func Submit(task func()) error {
	return defaultAntsPool.Submit(task)
}

// Running 获取默认线程池中正在运行的线程
func Running() int {
	return defaultAntsPool.Running()
}

// Cap 默认协程池大小
func Cap() int {
	return defaultAntsPool.Cap()
}

// Free 默认协程池可用线程数
func Free() int {
	return defaultAntsPool.Free()
}

// Release 关闭默认线程池
func Release() {
	_ = defaultAntsPool.Release()
}
