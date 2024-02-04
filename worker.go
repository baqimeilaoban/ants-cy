package ants_cy

import (
	"log"
	"time"
)

// Worker 任务的实际执行器，其负责起一个线程处理收到的任务
type Worker struct {
	pool        *Pool       // 该任务执行所属的协程池
	task        chan func() // 待执行的任务
	recycleTime time.Time   // 执行器放回队列后更新该时间
}

// run 协程执行任务
func (w *Worker) run() {
	w.pool.incrRunning()
	go func() {
		// 捕获异常
		defer func() {
			if p := recover(); p != nil {
				w.pool.decRunning()
				w.pool.workCache.Put(w)
				if w.pool.PanicHandler != nil {
					w.pool.PanicHandler(p)
				} else {
					log.Printf("执行器存在panic:%v", p)
				}
			}
		}()
		for f := range w.task {
			if nil == f {
				w.pool.decRunning()
				w.pool.workCache.Put(w)
				return
			}
			f()
			if ok := w.pool.revertWorker(w); !ok {
				break
			}
		}
	}()
}
