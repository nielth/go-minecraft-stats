// Command mc serves Minecraft player statistics as Prometheus metrics.
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const ticksPerSecond = 20

var (
	statDesc = prometheus.NewDesc(
		"minecraft_player_stat",
		"Player statistic, as in the game's Statistics screen",
		[]string{"player", "category", "stat"}, nil,
	)
	playTimeDesc = prometheus.NewDesc(
		"minecraft_player_play_time_seconds",
		"Time played",
		[]string{"player"}, nil,
	)
)

func getenv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok {
		return v
	}
	return fallback
}

type statsCollector struct {
	serverDir string
	world     string
}

// playerNames maps UUID -> player name, from the server's usercache.json.
func (c statsCollector) playerNames() map[string]string {
	names := map[string]string{}
	raw, err := os.ReadFile(filepath.Join(c.serverDir, "usercache.json"))
	if err != nil {
		return names
	}
	var entries []struct {
		UUID string `json:"uuid"`
		Name string `json:"name"`
	}
	if json.Unmarshal(raw, &entries) != nil {
		return names
	}
	for _, e := range entries {
		names[e.UUID] = e.Name
	}
	return names
}

// statsDir is world/players/stats on newer servers and world/stats on older ones.
func (c statsCollector) statsDir() string {
	dir := filepath.Join(c.serverDir, c.world, "players", "stats")
	if info, err := os.Stat(dir); err == nil && info.IsDir() {
		return dir
	}
	return filepath.Join(c.serverDir, c.world, "stats")
}

func (c statsCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- statDesc
	ch <- playTimeDesc
}

func (c statsCollector) Collect(ch chan<- prometheus.Metric) {
	names := c.playerNames()
	paths, _ := filepath.Glob(filepath.Join(c.statsDir(), "*.json"))
	sort.Strings(paths)
	for _, path := range paths {
		uuid := strings.TrimSuffix(filepath.Base(path), ".json")
		player, ok := names[uuid]
		if !ok {
			player = uuid
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var file struct {
			Stats map[string]map[string]float64 `json:"stats"`
		}
		if json.Unmarshal(raw, &file) != nil || file.Stats == nil {
			continue // being written right now; the next scrape gets it
		}
		for category, values := range file.Stats {
			for stat, value := range values {
				ch <- prometheus.MustNewConstMetric(
					statDesc, prometheus.GaugeValue, value, player,
					strings.TrimPrefix(category, "minecraft:"),
					strings.TrimPrefix(stat, "minecraft:"),
				)
			}
		}
		custom := file.Stats["minecraft:custom"]
		// play_time since 1.17; play_one_minute (also in ticks) before that.
		ticks, ok := custom["minecraft:play_time"]
		if !ok {
			ticks = custom["minecraft:play_one_minute"]
		}
		ch <- prometheus.MustNewConstMetric(
			playTimeDesc, prometheus.GaugeValue, ticks/ticksPerSecond, player,
		)
	}
}

func main() {
	c := statsCollector{
		serverDir: getenv("MC_SERVER_DIR", "/data"),
		world:     getenv("MC_WORLD", "world"),
	}
	port := getenv("PORT", "9225")

	reg := prometheus.NewRegistry()
	reg.MustRegister(
		c,
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)
	http.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))

	fmt.Printf("Serving Minecraft player stats from %s on :%s/metrics\n",
		filepath.Join(c.serverDir, c.world), port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}
