package ants_cy

import (
	"sync"
	"sync/atomic"
	"time"
)

// PoolWithFunc 自带函数处理的协程池
type PoolWithFunc struct {
	capacity         int32             // 协程池容量
	running          int32             // 运行中的线程
	expiryDuration   time.Duration     // 不活跃线程的清理时间
	workers          []*goWorkWithFunc // 执行器列表
	release          int32             // 是否清理协程池
	lock             sync.Locker       // 锁，保证并发安全
	cond             *sync.Cond        // 等待获取空闲执行器，用于线程间通信
	poolFunc         func(interface{}) // 执行任务的方法
	once             sync.Once         // 保证该线程池只会被关闭一次
	workCache        sync.Pool         // 提高性能，利用pool暂存数据
	panicHandler     func(interface{}) // panic后的处理方法
	maxBlockingTasks int32             // 最大阻塞任务数量
	blockingNum      int32             // 当前正阻塞任务数量
	nonblocking      bool              // 是否允许任务阻塞
}

// periodicallyPurge 定期清理不活跃执行器
func (p *PoolWithFunc) periodicallyPurge() {
	heatBeat := time.NewTicker(p.expiryDuration)
	defer heatBeat.Stop()
	var expiredWorkers []*goWorkWithFunc
	for range heatBeat.C {
		// 若是线程池已经被关闭，则直接退出清理，释放空间
		if CLOSE == atomic.LoadInt32(&p.release) {
			break
		}
		currentTime := time.Now()
		p.lock.Lock()
		idleWorkers := p.workers
		n := len(idleWorkers)
		i := 0
		for i < n && currentTime.Sub(idleWorkers[i].recycleTime) > p.expiryDuration {
			i++
		}
		// 已过期待删除执行器
		expiredWorkers = append(expiredWorkers[:0], idleWorkers[:i]...)
		if i > 0 {
			// 复制还未过期的执行器
			m := copy(idleWorkers, idleWorkers[i:])
			for i := m; i < n; i++ {
				idleWorkers[i] = nil
			}
			p.workers = idleWorkers[:m]
		}
		p.lock.Unlock()
		for i, w := range expiredWorkers {
			w.args <- nil
			expiredWorkers[i] = nil
		}
		if p.Running() == 0 {
			p.cond.Broadcast()
		}
	}
}

// NewPoolWithFunc 初始化协程池
func NewPoolWithFunc(size int, pf func(interface{}), options ...Option) (*PoolWithFunc, error) {
	if size <= 0 {
		return nil, ErrInvalidPoolSize
	}
	if pf == nil {
		return nil, ErrLackPoolFunc
	}
	opts := new(Options)
	for _, option := range options {
		option(opts)
	}
	if expiry := opts.ExpiryDuration; expiry < 0 {
		return nil, ErrInvalidPoolExpiry
	} else if expiry == 0 {
		opts.ExpiryDuration = time.Duration(DEFAULT_CLEAN_INERVAL_TIME) * time.Second
	}
	p := &PoolWithFunc{
		capacity:         int32(size),
		expiryDuration:   opts.ExpiryDuration,
		poolFunc:         pf,
		panicHandler:     opts.PanicHandler,
		maxBlockingTasks: int32(opts.MaxBlockingTasks),
		nonblocking:      opts.Nonblocking,
		lock:             SpinLock(),
	}
	if opts.PreAlloc {
		p.workers = make([]*goWorkWithFunc, 0, size)
	}
	p.cond = sync.NewCond(p.lock)
	go p.periodicallyPurge()
	return p, nil
}

// Invoke 提交任务到协程池
func (p *PoolWithFunc) Invoke(args interface{}) error {
	if CLOSE == atomic.LoadInt32(&p.release) {
		return ErrPoolClosed
	}
	if w := p.retrieveWorker(); w == nil {
		return ErrPoolOverload
	} else {
		w.args <- args
	}
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
}

// Release 关闭线程池
func (p *PoolWithFunc) Release() {
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
}

// incRunning 活跃线程数+1
func (p *PoolWithFunc) incRunning() {
	atomic.AddInt32(&p.running, 1)
}

// decRunning 线程数-1
func (p *PoolWithFunc) decRunning() {
	atomic.AddInt32(&p.running, -1)
}

// retrieveWorker 分配可用的执行器执行任务
func (p *PoolWithFunc) retrieveWorker() *goWorkWithFunc {
	var w *goWorkWithFunc
	spawnWorker := func() {
		if cacheWorker := p.workCache.Get(); cacheWorker != nil {
			w = cacheWorker.(*goWorkWithFunc)
		} else {
			w = &goWorkWithFunc{
				pool: p,
				args: make(chan interface{}, workChanCap()),
			}
		}
		w.run()
	}
	p.lock.Lock()
	idleWorkers := p.workers
	n := len(idleWorkers) - 1
	if n >= 0 {
		w = idleWorkers[n]
		idleWorkers[n] = nil
		p.workers = idleWorkers[:n]
		p.lock.Unlock()
	} else if p.Running() < p.Cap() {
		p.lock.Unlock()
		spawnWorker()
	} else {
		if p.nonblocking {
			p.lock.Unlock()
			return nil
		}
	Reentry:
		if p.maxBlockingTasks != 0 && p.blockingNum >= p.maxBlockingTasks {
			p.lock.Unlock()
			return nil
		}
		p.blockingNum++
		p.cond.Wait()
		p.blockingNum--
		// 若是现在待运行的执行器为0，那么直接生产一个即可，若是不直接生成一个，极端情况下会卡住
		if p.Running() == 0 {
			p.lock.Unlock()
			spawnWorker()
			return w
		}
		l := len(p.workers) - 1
		if l < 0 {
			goto Reentry
		}
		w = p.workers[l]
		p.workers[l] = nil
		p.workers = p.workers[:l]
		p.lock.Unlock()
	}
	return w
}

// 将执行器放回协程池中，并更新过期刷新时间
func (p *PoolWithFunc) revertWorker(worker *goWorkWithFunc) bool {
	if CLOSE == atomic.LoadInt32(&p.release) || p.Running() > p.Cap() {
		return false
	}
	worker.recycleTime = time.Now()
	p.lock.Lock()
	p.workers = append(p.workers, worker)
	p.cond.Signal()
	p.lock.Unlock()
	return true
}
