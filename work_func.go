package ants_cy

import "time"

// WorkWithFunc 携带函数的执行者
type WorkWithFunc struct {
	pool        *PoolWithFunc    // 该执行器所属的协程池
	args        chan interface{} // 要完成任务
	recycleTime time.Time        // 将执行器放入队列时，更新时间
}
