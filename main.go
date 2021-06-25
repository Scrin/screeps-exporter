package main

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type MemoryResponse struct {
	Ok   int64  `json:"ok"`
	Data string `json:"data"`
}

type Memory struct {
	Stats struct {
		History []Stats `json:"history"`
	} `json:"stats"`
}

type Cpu struct {
	Used   float64 `json:"used"`
	Limit  float64 `json:"limit"`
	Bucket float64 `json:"bucket"`
}

type Progress struct {
	Level         int64   `json:"level"`
	Progress      float64 `json:"progress"`
	ProgressTotal float64 `json:"progressTotal"`
}

type Room struct {
	RCL                     Progress         `json:"rcl"`
	Structures              map[string]int64 `json:"structures"`
	Creeps                  int64            `json:"creeps"`
	EnergyAvailable         float64          `json:"energyAvailable"`
	EnergyCapacityAvailable float64          `json:"energyCapacityAvailable"`
	Storage                 map[string]int64 `json:"storage"`
}

type Stats struct {
	Tick  uint64          `json:"tick"`
	Ms    uint64          `json:"ms"`
	CPU   Cpu             `json:"cpu"`
	GCL   Progress        `json:"gcl"`
	GPL   Progress        `json:"gpl"`
	Rooms map[string]Room `json:"rooms"`
}

const prefix = "screeps_"

var (
	shard = ""
	token = ""

	client = &http.Client{}

	knownRooms map[string]prometheus.Labels

	tick               *prometheus.GaugeVec
	ms                 *prometheus.GaugeVec
	cpu                *prometheus.GaugeVec
	gcl                *prometheus.GaugeVec
	gpl                *prometheus.GaugeVec
	rcl                *prometheus.GaugeVec
	energy             *prometheus.GaugeVec
	creeps             *prometheus.GaugeVec
	structures         *prometheus.GaugeVec
	storage            *prometheus.GaugeVec
	processingDuration prometheus.Histogram
)

func setup() {
	baseLabels := []string{"shard"}
	typedLabels := append(baseLabels, "type")
	roomLabels := append(baseLabels, "room")
	roomTypedLabels := append(typedLabels, "room")

	tick = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: prefix + "tick",
		Help: "Current tick",
	}, baseLabels)
	ms = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: prefix + "ms",
		Help: "Current time",
	}, baseLabels)
	cpu = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: prefix + "cpu",
		Help: "CPU statistics",
	}, typedLabels)
	gcl = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: prefix + "gcl",
		Help: "Global Control Level",
	}, typedLabels)
	gpl = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: prefix + "gpl",
		Help: "Global Power Level",
	}, typedLabels)
	rcl = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: prefix + "rcl",
		Help: "Room Control Level",
	}, roomTypedLabels)
	energy = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: prefix + "energy",
		Help: "Energy statistics",
	}, roomTypedLabels)
	creeps = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: prefix + "creeps",
		Help: "Creep counts",
	}, roomLabels)
	structures = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: prefix + "structures",
		Help: "Structure counts",
	}, roomTypedLabels)
	storage = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: prefix + "storage",
		Help: "Storage contents",
	}, roomTypedLabels)
	processingDuration = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    prefix + "stats_processing_time",
		Help:    "Time it has taken to process stats",
		Buckets: []float64{.001, .005, .01, .05, .1, .25, .5, 1, 2.5, 5, 10, 30, 60},
	})

	prometheus.MustRegister(tick)
	prometheus.MustRegister(ms)
	prometheus.MustRegister(cpu)
	prometheus.MustRegister(gcl)
	prometheus.MustRegister(gpl)
	prometheus.MustRegister(rcl)
	prometheus.MustRegister(energy)
	prometheus.MustRegister(creeps)
	prometheus.MustRegister(structures)
	prometheus.MustRegister(storage)
	prometheus.MustRegister(processingDuration)
}

func updateStats() {
	start := time.Now()
	req, err := http.NewRequest("GET", "https://screeps.com/api/user/memory?shard="+shard, nil)
	if err != nil {
		fmt.Println(err)
		return
	}
	req.Header.Set("X-Token", token)
	resp, err := client.Do(req)
	if err != nil {
		fmt.Println(err)
		return
	}
	defer resp.Body.Close()
	var memoryResponse MemoryResponse
	err = json.NewDecoder(resp.Body).Decode(&memoryResponse)
	if err != nil {
		fmt.Println(err)
		return
	}
	data := strings.TrimPrefix(memoryResponse.Data, "gz:")
	z, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		fmt.Println(err)
		return
	}
	r, err := gzip.NewReader(bytes.NewReader(z))
	if err != nil {
		fmt.Println(err)
		return
	}
	defer r.Close()
	var memory Memory
	err = json.NewDecoder(r).Decode(&memory)
	if err != nil {
		fmt.Println(err)
		return
	}

	stats := memory.Stats.History[len(memory.Stats.History)-1]

	tick.With(prometheus.Labels{"shard": shard}).Set(float64(stats.Tick))
	ms.With(prometheus.Labels{"shard": shard}).Set(float64(stats.Ms))

	cpu.With(prometheus.Labels{"shard": shard, "type": "used"}).Set(float64(stats.CPU.Used))
	cpu.With(prometheus.Labels{"shard": shard, "type": "limit"}).Set(float64(stats.CPU.Limit))
	cpu.With(prometheus.Labels{"shard": shard, "type": "bucket"}).Set(float64(stats.CPU.Bucket))

	gcl.With(prometheus.Labels{"shard": shard, "type": "level"}).Set(float64(stats.GCL.Level))
	gcl.With(prometheus.Labels{"shard": shard, "type": "progress"}).Set(float64(stats.GCL.Progress))
	gcl.With(prometheus.Labels{"shard": shard, "type": "progressTotal"}).Set(float64(stats.GCL.ProgressTotal))

	gpl.With(prometheus.Labels{"shard": shard, "type": "level"}).Set(float64(stats.GPL.Level))
	gpl.With(prometheus.Labels{"shard": shard, "type": "progress"}).Set(float64(stats.GPL.Progress))
	gpl.With(prometheus.Labels{"shard": shard, "type": "progressTotal"}).Set(float64(stats.GPL.ProgressTotal))

	rcl.Reset()
	energy.Reset()
	creeps.Reset()
	structures.Reset()
	storage.Reset()
	for name, room := range stats.Rooms {
		rcl.With(prometheus.Labels{"shard": shard, "room": name, "type": "level"}).Set(float64(room.RCL.Level))
		rcl.With(prometheus.Labels{"shard": shard, "room": name, "type": "progress"}).Set(float64(room.RCL.Progress))
		rcl.With(prometheus.Labels{"shard": shard, "room": name, "type": "progressTotal"}).Set(float64(room.RCL.ProgressTotal))

		energy.With(prometheus.Labels{"shard": shard, "room": name, "type": "available"}).Set(float64(room.EnergyAvailable))
		energy.With(prometheus.Labels{"shard": shard, "room": name, "type": "capacityAvailable"}).Set(float64(room.EnergyCapacityAvailable))

		creeps.With(prometheus.Labels{"shard": shard, "room": name}).Set(float64(room.Creeps))

		for structureType, count := range room.Structures {
			structures.With(prometheus.Labels{"shard": shard, "room": name, "type": structureType}).Set(float64(count))
		}
		for resourceType, amount := range room.Storage {
			storage.With(prometheus.Labels{"shard": shard, "room": name, "type": resourceType}).Set(float64(amount))
		}
	}
	processingDuration.Observe(time.Since(start).Seconds())
}

func main() {
	for _, e := range os.Environ() {
		split := strings.SplitN(e, "=", 2)
		switch split[0] {
		case "SCREEPS_SHARD":
			shard = split[1]
		case "SCREEPS_TOKEN":
			token = split[1]
		}
	}

	if len(os.Args) > 2 {
		shard = os.Args[1]
		token = os.Args[2]
	}

	if shard == "" || token == "" {
		log.Fatal("invalid config")
	}

	setup()
	go func() {
		for {
			updateStats()
			time.Sleep(time.Minute)
		}
	}()

	http.Handle("/metrics", promhttp.Handler())
	http.ListenAndServe(":8080", nil)
}
