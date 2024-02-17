package ants_cy

import (
	"sync"
	"sync/atomic"
	"time"
)

// Pool 协程池定义。该协程池接受来自客户端的任务，并且其能限制线程数量
type Pool struct {
	capacity         int32             // 协程池容量
	running          int32             // 协程池中活跃线程
	expiryDuration   time.Duration     // 不活跃线程的清理时间
	workers          []*goWorker       // 用于存储可用执行器的切片
	release          int32             // 用于通知协程池关闭
	lock             sync.Mutex        // 锁，保证并发安全
	cond             *sync.Cond        // 等待空闲执行器
	once             sync.Once         // 保证协程池的关闭只执行一次
	workCache        sync.Pool         // 用于加速获取可用执行器，引入golang的缓存池
	panicHandler     func(interface{}) // 用于捕捉panic
	maxBlockingTasks int32             // 最大允许提交的任务，0意味着没有限制
	blockingNum      int32             // 被阻塞的任务数
	nonblocking      bool              // 是否运行被阻塞，若为 true，则超限后，会返回 ErrPoolOverload 错误
}

// periodicallyPurge 定时清理过期执行器
func (p *Pool) periodicallyPurge() {
	// 初始化定时器
	heatBeat := time.NewTicker(p.expiryDuration)
	defer heatBeat.Stop()
	var expiredWorkers []*goWorker
	for range heatBeat.C {
		// 原子方式加载值，安全
		if CLOSE == atomic.LoadInt32(&p.release) {
			break
		}
		currentTime := time.Now()
		p.lock.Lock()
		// 空闲执行器
		idleWorkers := p.workers
		n := len(idleWorkers)
		var i int
		for i = 0; i < n && currentTime.Sub(idleWorkers[i].recycleTime) > p.expiryDuration; i++ {
		}
		expiredWorkers = append(expiredWorkers[:0], idleWorkers[:i]...)
		if i > 0 {
			m := copy(idleWorkers, idleWorkers[i:])
			for i := m; i < n; i++ {
				idleWorkers[i] = nil
			}
			p.workers = idleWorkers[:m]
		}
		p.lock.Unlock()
		for i, w := range expiredWorkers {
			w.task <- nil
			expiredWorkers[i] = nil
		}
		// 若是活跃的线程数为0，则通知所有等待的线程执行后续操作，若是不通知还在等待的线程，可能会造成内存泄漏
		// 并且部分任务无法得到执行，一直卡在等待中
		if p.Running() == 0 {
			p.cond.Broadcast()
		}
	}
}

// NewPool 初始化协程池，需指定协程池大小
func NewPool(size int, options ...Option) (*Pool, error) {
	if size <= 0 {
		return nil, ErrInvalidPoolSize
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
	p := &Pool{
		capacity:         int32(size),
		expiryDuration:   opts.ExpiryDuration,
		panicHandler:     opts.PanicHandler,
		maxBlockingTasks: int32(opts.MaxBlockingTasks),
		nonblocking:      opts.Nonblocking,
	}
	if opts.PreAlloc {
		p.workers = make([]*goWorker, 0, size)
	}
	p.cond = sync.NewCond(&p.lock)
	// 协程清理空闲执行器
	go p.periodicallyPurge()
	return p, nil
}

// Submit 提交任务到协程池中
func (p *Pool) Submit(task func()) error {
	// 若是协程池关闭，则返回错误
	if CLOSE == atomic.LoadInt32(&p.release) {
		return ErrPoolClosed
	}
	if w := p.retrieveWorker(); w == nil {
		return ErrPoolOverload
	} else {
		w.task <- task
	}
	return nil
}

// Running 返回当前正在运行中的线程数
func (p *Pool) Running() int {
	return int(atomic.LoadInt32(&p.running))
}

// Free 返回可用的线程数
func (p *Pool) Free() int {
	return int(atomic.LoadInt32(&p.capacity) - atomic.LoadInt32(&p.running))
}

// Cap 返回协程池的容量
func (p *Pool) Cap() int {
	return int(atomic.LoadInt32(&p.capacity))
}

// Tune 动态改变协程池大小
func (p *Pool) Tune(size int) {
	if size == p.Cap() {
		return
	}
	atomic.StoreInt32(&p.capacity, int32(size))
}

// Release 关闭协程池
func (p *Pool) Release() {
	// 使用 sync.Once 方法为了关闭安全，协程池只需要关闭一次即可
	p.once.Do(func() {
		atomic.StoreInt32(&p.release, 1)
		p.lock.Lock()
		idleWorks := p.workers
		for i, w := range idleWorks {
			w.task <- nil
			idleWorks[i] = nil
		}
		p.workers = nil
		p.lock.Unlock()
	})
}

// incrRunning 正在运行的线程数+1
func (p *Pool) incrRunning() {
	atomic.AddInt32(&p.running, 1)
}

// decRunning 正在运行的线程数-1
func (p *Pool) decRunning() {
	atomic.AddInt32(&p.running, -1)
}

// retrieveWorker 分配可用执行器执行任务
func (p *Pool) retrieveWorker() *goWorker {
	var w *goWorker
	spawnWorker := func() {
		if cacheWorker := p.workCache.Get(); cacheWorker != nil {
			w = cacheWorker.(*goWorker)
		} else {
			w = &goWorker{
				pool: p,
				task: make(chan func(), workChanCap()),
			}
		}
		w.run()
	}
	p.lock.Lock()
	idleWorkers := p.workers
	n := len(idleWorkers) - 1
	// 如果n大于0，先分配可用执行器
	if n >= 0 {
		// 未销毁的执行器，其内部进程还在继续运行
		w = idleWorkers[n]
		idleWorkers[n] = nil
		p.workers = idleWorkers[:n]
		p.lock.Unlock()
	} else if p.Running() < p.Cap() {
		// 真正分配并运行任务的执行器
		p.lock.Unlock()
		spawnWorker()
	} else {
		if p.nonblocking {
			p.lock.Unlock()
			return nil
		}
	Reentry:
		// 允许的最大阻塞长度，若是最大阻塞长度为0，则表示每个任务均阻塞
		if p.maxBlockingTasks != 0 && p.blockingNum >= p.maxBlockingTasks {
			p.lock.Unlock()
			return nil
		}
		p.blockingNum++
		p.cond.Wait()
		p.blockingNum--
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

// revertWorker 归还执行器入协程池中
func (p *Pool) revertWorker(worker *goWorker) bool {
	// 若是正在运行的线程数大于最大线程数，则该执行器退出，等待清理
	if CLOSE == atomic.LoadInt32(&p.release) || p.Running() > p.Cap() {
		return false
	}
	worker.recycleTime = time.Now()
	p.lock.Lock()
	p.workers = append(p.workers, worker)
	// 通知执行器空闲
	p.cond.Signal()
	p.lock.Unlock()
	return true
}
