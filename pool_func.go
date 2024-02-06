package ants_cy

import (
	"sync"
	"sync/atomic"
	"time"
)

// PoolWithFunc 自带函数处理的协程池
type PoolWithFunc struct {
	capacity       int32             // 协程池容量
	running        int32             // 运行中的线程
	expiryDuration time.Duration     // 不活跃线程的清理时间
	workers        []*WorkWithFunc   // 执行器列表
	release        int32             // 是否清理协程池
	lock           sync.Mutex        // 锁，保证并发安全
	cond           *sync.Cond        // 等待获取空闲执行器，用于线程间通信
	poolFunc       func(interface{}) // 执行任务的方法
	once           sync.Once         // 保证该线程池只会被关闭一次
	workCache      sync.Pool         // 提高性能，利用pool短暂存储数据
	PanicHandler   func(interface{}) // panic后的处理方法
}

// periodicallyPurge 定期清理不活跃执行器
func (p *PoolWithFunc) periodicallyPurge() {
	heatBeat := time.NewTicker(p.expiryDuration)
	defer heatBeat.Stop()
	for range heatBeat.C {
		if CLOSE == atomic.LoadInt32(&p.release) {
			break
		}
		currentTime := time.Now()
		p.lock.Lock()
		idleWorks := p.workers
		n := -1
		for i, w := range idleWorks {
			if currentTime.Sub(w.recycleTime) <= p.expiryDuration {
				break
			}
			n = i
			w.args <- nil
			idleWorks[i] = nil
		}
		if n > -1 {
			if n >= len(idleWorks)-1 {
				p.workers = idleWorks[:0]
			} else {
				p.workers = idleWorks[n+1:]
			}
		}
		p.lock.Unlock()
	}
}

func NewPoolWithFunc(size int, pf func(interface{})) (*PoolWithFunc, error) {
	return NewTimingPoolWithFunc(size, DEFAULT_CLEAN_INERVAL_TIME, pf)
}

// NewTimingPoolWithFunc 初始化协程池，指定size/expiry/pf
func NewTimingPoolWithFunc(size, expiry int, pf func(interface{})) (*PoolWithFunc, error) {
	if size < 0 {
		return nil, ErrInvalidPoolSize
	}
	if expiry < 0 {
		return nil, ErrInvalidPoolExpiry
	}
	p := &PoolWithFunc{
		capacity:       int32(size),
		expiryDuration: time.Duration(expiry) * time.Second,
		poolFunc:       pf,
	}
	p.cond = sync.NewCond(&p.lock)
	go p.periodicallyPurge()
	return p, nil
}

// Invoke 提交任务到协程池
func (p *PoolWithFunc) Invoke(args interface{}) error {
	if CLOSE == atomic.LoadInt32(&p.release) {
		return ErrPoolClosed
	}
	p.retrieveWorker().args <- args
	return nil
}

// Running 获取活跃线程数
func (p *PoolWithFunc) Running() int {
	return int(atomic.LoadInt32(&p.running))
}

// Free 获取空闲线程数
func (p *PoolWithFunc) Free() int {
	return int(atomic.LoadInt32(&p.capacity) - atomic.LoadInt32(&p.running))
}

// Cap 获取线程池容量
func (p *PoolWithFunc) Cap() int {
	return int(atomic.LoadInt32(&p.capacity))
}

// Tune 动态改变线程池大小
func (p *PoolWithFunc) Tune(size int) {
	if size == p.Cap() {
		return
	}
	atomic.StoreInt32(&p.capacity, int32(size))
	diff := p.Running() - size
	for i := 0; i < diff; i++ {
		p.retrieveWorker().args <- nil
	}
}

// Release 关闭线程池
func (p *PoolWithFunc) Release() error {
	p.once.Do(func() {
		atomic.StoreInt32(&p.release, 1)
		p.lock.Lock()
		idleWorkers := p.workers
		for i, w := range idleWorkers {
			w.args <- nil
			idleWorkers[i] = nil
		}
		p.workers = nil
		p.lock.Unlock()
	})
	return nil
}

// retrieveWorker 分配可用的执行器执行任务
func (p *PoolWithFunc) retrieveWorker() *WorkWithFunc {
	var w *WorkWithFunc
	p.lock.Lock()
	idleWorkers := p.workers
	n := len(idleWorkers) - 1
	if n >= 0 {
		w = idleWorkers[n]
		idleWorkers[n] = nil
		p.workers = idleWorkers[:n]
		p.lock.Unlock()
	} else if p.running < p.capacity {
		p.lock.Unlock()
		if cacheWorker := p.workCache.Get(); cacheWorker != nil {
			w = cacheWorker.(*WorkWithFunc)
		} else {
			w = &WorkWithFunc{
				pool: p,
				args: make(chan interface{}, workChanCap()),
			}
		}
		w.run()
	} else {
		for {
			p.cond.Wait()
			l := len(p.workers) - 1
			if l < 0 {
				continue
			}
			w = p.workers[l]
			p.workers[l] = nil
			p.workers = p.workers[:l]
			break
		}
		p.lock.Unlock()
	}
	return w
}

// incRunning 活跃线程数+1
func (p *PoolWithFunc) incRunning() {
	atomic.AddInt32(&p.running, 1)
}

// decRunning 线程数-1
func (p *PoolWithFunc) decRunning() {
	atomic.AddInt32(&p.running, -1)
}

// 将执行器放回协程池中，并更新过期刷新时间
func (p *PoolWithFunc) revertWorker(worker *WorkWithFunc) bool {
	if CLOSE == atomic.LoadInt32(&p.release) {
		return false
	}
	worker.recycleTime = time.Now()
	p.lock.Lock()
	p.workers = append(p.workers, worker)
	p.cond.Signal()
	p.lock.Unlock()
	return true
}
