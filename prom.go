package main

import (
	"log"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

var (
	succCount   uint64
	succCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "llr_success_count",
			Help: "consume success count",
		},
		[]string{"key"},
	)

	idleCount   uint64
	idleCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "llr_idle_count",
			Help: "message queue empty will sleep thread, total sleep count",
		},
		[]string{},
	)

	dirtyCount   uint64
	dirtyCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "llr_dirty_data_count",
			Help: "item unmarshal failure, wrong log data struct",
		},
		[]string{},
	)

	wrongMetaDataCount   uint64
	wrongMetaDataCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "llr_wrong_meta_data_count",
			Help: "report loki label must be string",
		},
		[]string{},
	)
)

func init() {
	prometheus.MustRegister(succCounter)
	prometheus.MustRegister(idleCounter)
	prometheus.MustRegister(dirtyCounter)
	prometheus.MustRegister(wrongMetaDataCounter)
}

func promSuccCountInc(key string) {
	atomic.AddUint64(&succCount, 1)
	succCounter.WithLabelValues(key).Inc()
}

func promIdleCountInc() {
	atomic.AddUint64(&idleCount, 1)
	idleCounter.With(prometheus.Labels{}).Inc()
}

func promDirtyCountInc() {
	atomic.AddUint64(&dirtyCount, 1)
	dirtyCounter.With(prometheus.Labels{}).Inc()
}

func promWrongMetaDataInc() {
	atomic.AddUint64(&wrongMetaDataCount, 1)
	wrongMetaDataCounter.With(prometheus.Labels{}).Inc()
}

func printConsumeStat() {
	blockD := 10 * time.Second
	for {
		log.Printf("succ=%d, idle=%d, dirty=%d, wrong_meta=%d",
			succCount, idleCount, dirtyCount, wrongMetaDataCount)
		time.Sleep(blockD)
	}
}
