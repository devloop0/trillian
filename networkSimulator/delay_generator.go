package NetworkSimulator

import (
	"gonum.org/v1/gonum/stat/distuv"
	"time"
)

const burstyBehavior bool = true

const meanDelayMs float64 = 100
var networkDelaySource distuv.Poisson = distuv.Poisson{Lambda: meanDelayMs}

const networkDropProb float64 = 0.0001
var networkDropSource distuv.Bernoulli = distuv.Bernoulli{P: networkDropProb}

const burstyMeanDelayMs float64 = 1
const burstyStdDev float64 = 0.1
var burstyNetworkDelaySource distuv.Normal = distuv.Normal{Mu: burstyMeanDelayMs, Sigma: burstyStdDev}

const burstyProbability float64 = 0.001
var burstyBehaviorEntrySource distuv.Bernoulli = distuv.Bernoulli{P: burstyProbability}

const burstyMeanLimit float64 = 100
var burstyLimitSource distuv.Poisson = distuv.Poisson{Lambda: burstyMeanLimit}
var burstyLimit float64 = 0
var numBurstyRequests float64 = 0
var burstyState bool = false

func DropPacket() bool {
	return networkDropSource.Rand() == 0
}

func GenerateDelay() {
	if !burstyBehavior {
		time.Sleep(time.Duration(networkDelaySource.Rand()) * time.Millisecond)
	} else {
		if !burstyState {
			burstyState = burstyBehaviorEntrySource.Rand() == 1
			if burstyState {
				numBurstyRequests = 0
				burstyLimit = burstyLimitSource.Rand()
			}
		}
		if burstyState {
			numBurstyRequests += 1
			time.Sleep(time.Duration(burstyNetworkDelaySource.Rand()) * time.Millisecond)
			burstyState = numBurstyRequests < burstyLimit
		} else {
			time.Sleep(time.Duration(networkDelaySource.Rand()) * time.Millisecond)
		}
	}
}
