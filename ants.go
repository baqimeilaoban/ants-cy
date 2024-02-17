package ants_cy

import (
	"errors"
	"math"
	"runtime"
	"time"
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
	ErrLackPoolFunc      = errors.New("协程池中必须提供函数")
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

// Options 引入option参数
type Options struct {
	ExpiryDuration   time.Duration     // 不活跃线程的清理时间
	PreAlloc         bool              // 是否需要提前为执行器分配内存
	MaxBlockingTasks int               // 最大允许提交的任务，0意味着没有限制
	Nonblocking      bool              // 是否运行被阻塞，若为 true，则超限后，会返回 ErrPoolOverload 错误
	PanicHandler     func(interface{}) // 用于捕捉panic
}

// Option Option函数
type Option func(opts *Options)

// WithOptions 构造options函数
func WithOptions(options Options) Option {
	return func(opts *Options) {
		*opts = options
	}
}

// WithExpiryDuration 构造清理不活跃线程的清理时间
func WithExpiryDuration(expiryDuration time.Duration) Option {
	return func(opts *Options) {
		opts.ExpiryDuration = expiryDuration
	}
}

// WithPreAlloc 构造是否提前分配内存的函数
func WithPreAlloc(preAlloc bool) Option {
	return func(opts *Options) {
		opts.PreAlloc = preAlloc
	}
}

// WithMaxBlockingTasks 构造最大允许提交任务的函数
func WithMaxBlockingTasks(maxBlockingTasks int) Option {
	return func(opts *Options) {
		opts.MaxBlockingTasks = maxBlockingTasks
	}
}

// WithNonblocking 构造是否允许阻塞的函数
func WithNonblocking(nonblocking bool) Option {
	return func(opts *Options) {
		opts.Nonblocking = nonblocking
	}
}

// WithPanicHandler 构造panic后的处理函数
func WithPanicHandler(panicHandler func(interface{})) Option {
	return func(opts *Options) {
		opts.PanicHandler = panicHandler
	}
}

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
	defaultAntsPool.Release()
}
