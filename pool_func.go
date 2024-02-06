package ants_cy

import (
	"sync"
	"time"
)

// PoolWithFunc 自带函数处理的协程池
type PoolWithFunc struct {
	capacity       int32          // 协程池容量
	running        int32          // 运行中的线程
	expiryDuration time.Duration  // 不活跃线程的清理时间
	workers        []WorkWithFunc // 执行器列表
	release        int32          // 是否清理协程池
	lock           sync.Mutex     // 锁，保证并发安全
	cond           *sync.Cond     // 等待获取空闲执行器

}
