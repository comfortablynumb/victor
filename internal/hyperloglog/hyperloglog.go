package hyperloglog

import (
	"sync"

	"github.com/axiomhq/hyperloglog"
)

// Structs

type HyperLogLog struct {
	sketch *hyperloglog.Sketch
	mutex  *sync.RWMutex
}

func (h *HyperLogLog) Insert(tags string) {
	h.mutex.Lock()
	defer h.mutex.Unlock()

	h.sketch.Insert([]byte(tags))
}

func (h *HyperLogLog) Estimate() uint64 {
	h.mutex.RLock()
	defer h.mutex.RUnlock()

	return h.sketch.Estimate()
}

// Static functions

func NewHyperLogLog(tags string) *HyperLogLog {
	sketch := hyperloglog.New14()

	sketch.Insert([]byte(tags))

	return &HyperLogLog{
		sketch: sketch,
		mutex:  &sync.RWMutex{},
	}
}
