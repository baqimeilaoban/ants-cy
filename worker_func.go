package ants_cy

import (
	"log"
	"runtime"
	"time"
)

// WorkWithFunc 携带函数的执行者
type WorkWithFunc struct {
	pool        *PoolWithFunc    // 该执行器所属的协程池
	args        chan interface{} // 要完成任务
	recycleTime time.Time        // 将执行器放入队列时，更新时间
}

// 执行器的实际执行
func (w *WorkWithFunc) run() {
	w.pool.incRunning()
	go func() {
		defer func() {
			if p := recover(); p != nil {
				w.pool.decRunning()
				// 归还到缓存池中
				w.pool.workCache.Put(w)
				if w.pool.PanicHandler != nil {
					w.pool.PanicHandler(p)
				} else {
					log.Printf("worker exit from panic:%v", p)
					var buf [4096]byte
					n := runtime.Stack(buf[:], false)
					log.Printf("worker with fun exits from panic:%s\n", string(buf[:n]))
				}
			}
		}()
		for args := range w.args {
			if nil == args {
				w.pool.decRunning()
				w.pool.workCache.Put(w)
				return
			}
			w.pool.poolFunc(args)
			if ok := w.pool.revertWorker(w); !ok {
				break
			}
		}
	}()
}