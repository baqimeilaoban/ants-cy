package ants_cy

import (
	"log"
	"runtime"
	"time"
)

// goWorkWithFunc 携带函数的执行者
type goWorkWithFunc struct {
	pool        *PoolWithFunc    // 该执行器所属的协程池
	args        chan interface{} // 要完成任务
	recycleTime time.Time        // 将执行器放入队列时，更新时间
}

// 执行器的实际执行
func (w *goWorkWithFunc) run() {
	w.pool.incRunning()
	go func() {
		defer func() {
			w.pool.decRunning()
			if p := recover(); p != nil {
				if w.pool.panicHandler != nil {
					w.pool.panicHandler(p)
				} else {
					log.Printf("worker exit from panic:%v", p)
					var buf [4096]byte
					n := runtime.Stack(buf[:], false)
					log.Printf("worker with fun exits from panic:%s\n", string(buf[:n]))
				}
			}
			// 归还到缓存池中
			w.pool.workCache.Put(w)
		}()
		for args := range w.args {
			if nil == args {
				return
			}
			w.pool.poolFunc(args)
			if ok := w.pool.revertWorker(w); !ok {
				return
			}
		}
	}()
}
