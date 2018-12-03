package UserTypes

import (
	"sync/atomic"
)

var counter int64 = 0

func GenerateTransactionId() int64 {
	ret := atomic.LoadInt64(&counter)
	atomic.AddInt64(&counter, 1)
	return ret
}

