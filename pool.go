package ants_cy

import (
	"sync"
	"sync/atomic"
	"time"
)

// Pool 协程池定义。该协程池接受来自客户端的任务，并且其能限制线程数量
type Pool struct {
	capacity       int32             // 协程池容量
	running        int32             // 协程池中活跃线程
	expiryDuration time.Duration     // 不活跃线程的清理时间
	workers        []*Worker         // 用于存储可用执行器的切片
	release        int32             // 用于通知协程池关闭
	lock           sync.Mutex        // 锁，保证并发安全
	cond           *sync.Cond        // 等待空闲执行器
	once           sync.Once         // 保证协程池的关闭只执行一次
	workCache      sync.Pool         // 用于加速获取可用执行器，引入golang的缓存池
	PanicHandler   func(interface{}) // 用于捕捉panic
}

// NewPool 初始化协程池，需指定协程池大小
func NewPool(size int) (*Pool, error) {
	return NewUltimatePool(size, DEFAULT_CLEAN_INERVAL_TIME, false)
}

// NewPoolPreMalloc 创建预分配空间的协程池
func NewPoolPreMalloc(size int) (*Pool, error) {
	return NewUltimatePool(size, DEFAULT_CLEAN_INERVAL_TIME, true)
}

// NewUltimatePool 初始化协程池，需指定协程池大小，并且定义不活跃线程的清理时间
func NewUltimatePool(size, expiry int, preAlloc bool) (*Pool, error) {
	if size < 0 {
		return nil, ErrInvalidPoolSize
	}
	if expiry < 0 {
		return nil, ErrInvalidPoolExpiry
	}
	var p *Pool
	if preAlloc {
		p = &Pool{
			capacity:       int32(size),
			expiryDuration: time.Duration(expiry) * time.Second,
			workers:        make([]*Worker, 0, size), // 预分配内存
		}
	} else {
		p = &Pool{
			capacity:       int32(size),
			expiryDuration: time.Duration(expiry) * time.Second,
		}
	}
	p.cond = sync.NewCond(&p.lock)
	// 协程清理空闲执行器
	go p.periodicallyPurge()
	return p, nil
}

// periodicallyPurge 定时清理过期执行器
func (p *Pool) periodicallyPurge() {
	// 初始化定时器
	heatBeat := time.NewTicker(p.expiryDuration)
	defer heatBeat.Stop()
	var expiredWorkers []*Worker
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
		i := 0
		for i < n && currentTime.Sub(idleWorkers[i].recycleTime) > p.expiryDuration {
			i++
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
	}
}

// Submit 提交任务到协程池中
func (p *Pool) Submit(task func()) error {
	// 若是协程池关闭，则返回错误
	if CLOSE == atomic.LoadInt32(&p.release) {
		return ErrPoolClosed
	}
	p.retrieveWorker().task <- task
	return nil
}

// Free 返回可用的线程数
func (p *Pool) Free() int {
	return int(atomic.LoadInt32(&p.capacity) - atomic.LoadInt32(&p.running))
}

// Tune 动态改变协程池大小
func (p *Pool) Tune(size int) {
	if size == p.Cap() {
		return
	}
	atomic.StoreInt32(&p.capacity, int32(size))
	diff := p.Running() - size
	for i := 0; i < diff; i++ {
		p.retrieveWorker().task <- nil
	}
}

// Release 关闭协程池
func (p *Pool) Release() error {
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
	return nil
}

// Running 返回当前正在运行中的线程数
func (p *Pool) Running() int {
	return int(atomic.LoadInt32(&p.running))
}

// retrieveWorker 分配可用执行器执行任务
func (p *Pool) retrieveWorker() *Worker {
	var w *Worker
	p.lock.Lock()
	idleWorkers := p.workers
	n := len(idleWorkers) - 1
	// 如果n大于0，先分配可用执行器
	if n >= 0 {
		// 未销户的执行器，其内部进程还在继续运行
		w = idleWorkers[n]
		idleWorkers[n] = nil
		p.workers = idleWorkers[:n]
		p.lock.Unlock()
	} else if p.Running() < p.Cap() {
		// 真正分配并运行任务的执行器
		p.lock.Unlock()
		if cacheWorker := p.workCache.Get(); cacheWorker != nil {
			w = cacheWorker.(*Worker)
		} else {
			w = &Worker{
				pool: p,
				task: make(chan func(), workChanCap()),
			}
		}
		// 任务真正开始执行
		w.run()
	} else {
	Reentry:
		p.cond.Wait()
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

// Cap 返回协程池的容量
func (p *Pool) Cap() int {
	return int(atomic.LoadInt32(&p.capacity))
}

// incrRunning 正在运行的线程数+1
func (p *Pool) incrRunning() {
	atomic.AddInt32(&p.running, 1)
}

// decRunning 正在运行的线程数-1
func (p *Pool) decRunning() {
	atomic.AddInt32(&p.running, -1)
}

// revertWorker 归还执行器入协程池中
func (p *Pool) revertWorker(worker *Worker) bool {
	if CLOSE == atomic.LoadInt32(&p.release) {
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
