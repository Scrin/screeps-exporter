package main

import (
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

type AuthMeResponse struct {
	Money     float64            `json:"money"`
	CPUShard  map[string]float64 `json:"cpuShard"`
	Resources map[string]float64 `json:"resources"`
}

type MarketOrder struct {
	Active          bool    `json:"active"`
	Type            string  `json:"type"`
	Amount          float64 `json:"amount"`
	RemainingAmount float64 `json:"remainingAmount"`
	ResourceType    string  `json:"resourceType"`
	Price           float64 `json:"price"`
	TotalAmount     float64 `json:"totalAmount"`
	RoomName        string  `json:"roomName"`
}

type MarketOrdersResponse struct {
	Shards map[string][]MarketOrder `json:"shards"`
}

type MemoryResponse struct {
	Ok   float64 `json:"ok"`
	Data string  `json:"data"`
}

type Cpu struct {
	Used   float64 `json:"used"`
	Limit  float64 `json:"limit"`
	Bucket float64 `json:"bucket"`
}

type Progress struct {
	Level         float64 `json:"level"`
	Progress      float64 `json:"progress"`
	ProgressTotal float64 `json:"progressTotal"`
}

type Room struct {
	RCL                     Progress           `json:"rcl"`
	Structures              map[string]float64 `json:"structures"`
	Creeps                  float64            `json:"creeps"`
	EnergyAvailable         float64            `json:"energyAvailable"`
	EnergyCapacityAvailable float64            `json:"energyCapacityAvailable"`
	Storage                 map[string]float64 `json:"storage"`
	Terminal                map[string]float64 `json:"terminal"`
}

type Stats struct {
	Tick                float64         `json:"tick"`
	Ms                  float64         `json:"ms"`
	LastGlobalResetTick float64         `json:"lastGlobalResetTick"`
	LastGlobalResetMs   float64         `json:"lastGlobalResetMs"`
	CPU                 Cpu             `json:"cpu"`
	GCL                 Progress        `json:"gcl"`
	GPL                 Progress        `json:"gpl"`
	Rooms               map[string]Room `json:"rooms"`
}

const prefix = "screeps_"

var (
	shards  []string
	segment = "0"
	token   = ""

	client = &http.Client{}

	knownRooms map[string]prometheus.Labels

	cpuShard           *prometheus.GaugeVec
	resources          *prometheus.GaugeVec
	marketOrders       *prometheus.GaugeVec
	tick               *prometheus.GaugeVec
	ms                 *prometheus.GaugeVec
	resetTick          *prometheus.GaugeVec
	resetMs            *prometheus.GaugeVec
	cpu                *prometheus.GaugeVec
	gcl                *prometheus.GaugeVec
	gpl                *prometheus.GaugeVec
	rcl                *prometheus.GaugeVec
	energy             *prometheus.GaugeVec
	creeps             *prometheus.GaugeVec
	structures         *prometheus.GaugeVec
	storage            *prometheus.GaugeVec
	terminal           *prometheus.GaugeVec
	processingDuration prometheus.Histogram
)

func setup() {
	baseLabels := []string{"shard"}
	typedLabels := append(baseLabels, "type")
	roomLabels := append(baseLabels, "room")
	roomTypedLabels := append(typedLabels, "room")
	marketOrderLabels := append(roomTypedLabels, "order_type", "metric")

	cpuShard = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: prefix + "cpu_shard",
		Help: "CPU allocated to each shard",
	}, baseLabels)
	resources = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: prefix + "resources",
		Help: "Resource amounts",
	}, typedLabels)
	marketOrders = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: prefix + "market_orders",
		Help: "Market orders",
	}, marketOrderLabels)
	tick = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: prefix + "tick",
		Help: "Current tick",
	}, baseLabels)
	ms = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: prefix + "ms",
		Help: "Current time",
	}, baseLabels)
	resetTick = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: prefix + "global_reset_tick",
		Help: "Last global reset tick",
	}, baseLabels)
	resetMs = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: prefix + "global_reset_ms",
		Help: "Last global reset time",
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
	terminal = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: prefix + "terminal",
		Help: "Terminal contents",
	}, roomTypedLabels)
	processingDuration = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    prefix + "stats_processing_time",
		Help:    "Time it has taken to process stats",
		Buckets: []float64{.001, .005, .01, .05, .1, .25, .5, 1, 2.5, 5, 10, 30, 60},
	})

	prometheus.MustRegister(cpuShard)
	prometheus.MustRegister(resources)
	prometheus.MustRegister(marketOrders)
	prometheus.MustRegister(tick)
	prometheus.MustRegister(ms)
	prometheus.MustRegister(resetTick)
	prometheus.MustRegister(resetMs)
	prometheus.MustRegister(cpu)
	prometheus.MustRegister(gcl)
	prometheus.MustRegister(gpl)
	prometheus.MustRegister(rcl)
	prometheus.MustRegister(energy)
	prometheus.MustRegister(creeps)
	prometheus.MustRegister(structures)
	prometheus.MustRegister(storage)
	prometheus.MustRegister(terminal)
	prometheus.MustRegister(processingDuration)
}

func getStatsFromAuthMe() (AuthMeResponse, error) {
	req, err := http.NewRequest("GET", "https://screeps.com/api/auth/me", nil)
	if err != nil {
		return AuthMeResponse{}, err
	}
	req.Header.Set("X-Token", token)
	resp, err := client.Do(req)
	if err != nil {
		return AuthMeResponse{}, err
	}
	defer resp.Body.Close()
	var parsed AuthMeResponse
	err = json.NewDecoder(resp.Body).Decode(&parsed)
	if err != nil {
		return AuthMeResponse{}, err
	}
	return parsed, nil
}

func getMarketOrders() (MarketOrdersResponse, error) {
	req, err := http.NewRequest("GET", "https://screeps.com/api/game/market/my-orders", nil)
	if err != nil {
		return MarketOrdersResponse{}, err
	}
	req.Header.Set("X-Token", token)
	resp, err := client.Do(req)
	if err != nil {
		return MarketOrdersResponse{}, err
	}
	defer resp.Body.Close()
	var parsed MarketOrdersResponse
	err = json.NewDecoder(resp.Body).Decode(&parsed)
	if err != nil {
		return MarketOrdersResponse{}, err
	}
	return parsed, nil
}

func getStatsFromMemorySegment(shard string) (Stats, error) {
	req, err := http.NewRequest("GET", "https://screeps.com/api/user/memory-segment?segment="+segment+"&shard="+shard, nil)
	if err != nil {
		return Stats{}, err
	}
	req.Header.Set("X-Token", token)
	resp, err := client.Do(req)
	if err != nil {
		return Stats{}, err
	}
	defer resp.Body.Close()
	var memoryResponse MemoryResponse
	err = json.NewDecoder(resp.Body).Decode(&memoryResponse)
	if err != nil {
		return Stats{}, err
	}
	var parsed Stats
	err = json.Unmarshal([]byte(memoryResponse.Data), &parsed)
	if err != nil {
		return Stats{}, err
	}
	return parsed, nil
}

func updateStats() {
	start := time.Now()

	authMe, err := getStatsFromAuthMe()
	if err != nil {
		fmt.Println(err)
		return
	}
	orders, err := getMarketOrders()
	if err != nil {
		fmt.Println(err)
		return
	}
	var statsMap = make(map[string]Stats)
	for _, shard := range shards {
		stats, err := getStatsFromMemorySegment(shard)
		if err != nil {
			fmt.Println(err)
			return
		}
		statsMap[shard] = stats
	}

	cpuShard.Reset()
	resources.Reset()
	marketOrders.Reset()
	rcl.Reset()
	energy.Reset()
	creeps.Reset()
	structures.Reset()
	storage.Reset()
	terminal.Reset()

	for shard, amount := range authMe.CPUShard {
		cpuShard.With(prometheus.Labels{"shard": shard}).Set(amount)
	}
	resources.With(prometheus.Labels{"shard": "intershard", "type": "money"}).Set(authMe.Money)
	for typ, amount := range authMe.Resources {
		resources.With(prometheus.Labels{"shard": "intershard", "type": typ}).Set(amount)
	}

	for shard, orders := range orders.Shards {
		for _, order := range orders {
			marketOrders.With(prometheus.Labels{"shard": shard, "room": order.RoomName, "type": order.ResourceType, "order_type": order.Type, "metric": "price"}).Set(order.Price)
			marketOrders.With(prometheus.Labels{"shard": shard, "room": order.RoomName, "type": order.ResourceType, "order_type": order.Type, "metric": "amount"}).Set(order.Amount)
			marketOrders.With(prometheus.Labels{"shard": shard, "room": order.RoomName, "type": order.ResourceType, "order_type": order.Type, "metric": "remainingAmount"}).Set(order.RemainingAmount)
			marketOrders.With(prometheus.Labels{"shard": shard, "room": order.RoomName, "type": order.ResourceType, "order_type": order.Type, "metric": "totalAmount"}).Set(order.TotalAmount)
		}
	}

	for _, shard := range shards {
		var stats = statsMap[shard]
		tick.With(prometheus.Labels{"shard": shard}).Set(stats.Tick)
		ms.With(prometheus.Labels{"shard": shard}).Set(stats.Ms)
		resetTick.With(prometheus.Labels{"shard": shard}).Set(stats.LastGlobalResetTick)
		resetMs.With(prometheus.Labels{"shard": shard}).Set(stats.LastGlobalResetMs)

		cpu.With(prometheus.Labels{"shard": shard, "type": "used"}).Set(stats.CPU.Used)
		cpu.With(prometheus.Labels{"shard": shard, "type": "limit"}).Set(stats.CPU.Limit)
		cpu.With(prometheus.Labels{"shard": shard, "type": "bucket"}).Set(stats.CPU.Bucket)

		gcl.With(prometheus.Labels{"shard": shard, "type": "level"}).Set(stats.GCL.Level)
		gcl.With(prometheus.Labels{"shard": shard, "type": "progress"}).Set(stats.GCL.Progress)
		gcl.With(prometheus.Labels{"shard": shard, "type": "progressTotal"}).Set(stats.GCL.ProgressTotal)

		gpl.With(prometheus.Labels{"shard": shard, "type": "level"}).Set(stats.GPL.Level)
		gpl.With(prometheus.Labels{"shard": shard, "type": "progress"}).Set(stats.GPL.Progress)
		gpl.With(prometheus.Labels{"shard": shard, "type": "progressTotal"}).Set(stats.GPL.ProgressTotal)

		for name, room := range stats.Rooms {
			rcl.With(prometheus.Labels{"shard": shard, "room": name, "type": "level"}).Set(room.RCL.Level)
			rcl.With(prometheus.Labels{"shard": shard, "room": name, "type": "progress"}).Set(room.RCL.Progress)
			rcl.With(prometheus.Labels{"shard": shard, "room": name, "type": "progressTotal"}).Set(room.RCL.ProgressTotal)

			energy.With(prometheus.Labels{"shard": shard, "room": name, "type": "available"}).Set(room.EnergyAvailable)
			energy.With(prometheus.Labels{"shard": shard, "room": name, "type": "capacityAvailable"}).Set(room.EnergyCapacityAvailable)

			creeps.With(prometheus.Labels{"shard": shard, "room": name}).Set(room.Creeps)

			for structureType, count := range room.Structures {
				structures.With(prometheus.Labels{"shard": shard, "room": name, "type": structureType}).Set(count)
			}
			for resourceType, amount := range room.Storage {
				storage.With(prometheus.Labels{"shard": shard, "room": name, "type": resourceType}).Set(amount)
			}
			for resourceType, amount := range room.Terminal {
				terminal.With(prometheus.Labels{"shard": shard, "room": name, "type": resourceType}).Set(amount)
			}
		}
	}
	processingDuration.Observe(time.Since(start).Seconds())
}

func main() {
	for _, e := range os.Environ() {
		split := strings.SplitN(e, "=", 2)
		switch split[0] {
		case "SCREEPS_SHARDS":
			shards = strings.Split(split[1], ",")
		case "SCREEPS_SEGMENT":
			segment = split[1]
		case "SCREEPS_TOKEN":
			token = split[1]
		}
	}

	if len(os.Args) > 2 {
		shards = strings.Split(os.Args[1], ",")
		token = os.Args[2]
	}

	if len(shards) < 1 || token == "" {
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
