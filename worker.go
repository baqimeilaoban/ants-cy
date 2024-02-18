package ants_cy

import (
	"log"
	"runtime"
	"time"
)

// goWorker 任务的实际执行器，其负责起一个线程处理收到的任务
type goWorker struct {
	pool        *Pool       // 该任务执行所属的协程池
	task        chan func() // 待执行的任务
	recycleTime time.Time   // 执行器放回队列后更新该时间
}

// run 协程执行任务
func (w *goWorker) run() {
	w.pool.incrRunning()
	go func() {
		// 捕获异常
		defer func() {
			w.pool.decRunning()
			if p := recover(); p != nil {
				w.pool.workCache.Put(w)
				if w.pool.panicHandler != nil {
					w.pool.panicHandler(p)
				} else {
					// 新增panic打印的堆栈信息
					log.Printf("执行器存在panic:%v", p)
					var buf [4096]byte
					n := runtime.Stack(buf[:], false)
					log.Printf("执行器panic位置如下:%s\n", string(buf[:n]))
				}
			}
			w.pool.workCache.Put(w)
		}()
		for f := range w.task {
			if nil == f {
				return
			}
			f()
			if ok := w.pool.revertWorker(w); !ok {
				return
			}
		}
	}()
}
