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
)

var (
	workChanCap = func() int {
		if runtime.GOMAXPROCS(0) == 1 {
			return 0
		}
		return 1
	}
)
