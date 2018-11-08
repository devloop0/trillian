package NetworkSimulator

import (
	"gonum.org/v1/gonum/stat/distuv"
	"time"
)

const meanDelayMs float64 = 100
const networkDropProb float64 = 0.0001
var networkDelaySource distuv.Poisson = distuv.Poisson{Lambda: meanDelayMs}
var networkDropSource distuv.Bernoulli = distuv.Bernoulli{P: networkDropProb}

func DropPacket() bool {
	if networkDropSource.Rand() == 0 {
		return true
	} else {
		return false
	}
}

func GenerateDelay() {
	time.Sleep(time.Duration(networkDelaySource.Rand()) * time.Millisecond)
}
