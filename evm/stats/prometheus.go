// Copyright (c) 2025 Lux Partners Limited
// SPDX-License-Identifier: MIT

// Package stats provides Prometheus metrics export for blockchain statistics.
package stats

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// MetricsExporter exports statistics as Prometheus metrics
type MetricsExporter struct {
	service *Service
	mu      sync.RWMutex
	metrics map[string]float64
}

// NewMetricsExporter creates a new Prometheus metrics exporter
func NewMetricsExporter(service *Service) *MetricsExporter {
	return &MetricsExporter{
		service: service,
		metrics: make(map[string]float64),
	}
}

// UpdateMetrics refreshes all metric values
func (m *MetricsExporter) UpdateMetrics(ctx context.Context) error {
	// Get counters
	counters, err := m.service.GetCounters(ctx)
	if err != nil {
		return fmt.Errorf("get counters: %w", err)
	}

	// Get gas prices
	gasPrices, err := m.service.GetGasPrices(ctx)
	if err != nil {
		return fmt.Errorf("get gas prices: %w", err)
	}

	// Get block time
	blockTime, err := m.service.GetAverageBlockTime(ctx)
	if err != nil {
		return fmt.Errorf("get block time: %w", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Counter metrics
	m.metrics["lux_indexer_blocks_total"] = float64(counters.TotalBlocks)
	m.metrics["lux_indexer_transactions_total"] = float64(counters.TotalTransactions)
	m.metrics["lux_indexer_addresses_total"] = float64(counters.TotalAddresses)
	m.metrics["lux_indexer_contracts_total"] = float64(counters.TotalContracts)
	m.metrics["lux_indexer_tokens_total"] = float64(counters.TotalTokens)
	m.metrics["lux_indexer_token_transfers_total"] = float64(counters.TotalTokenTransfers)
	m.metrics["lux_indexer_verified_contracts_total"] = float64(counters.VerifiedContracts)
	m.metrics["lux_indexer_tps_24h"] = counters.TPS24h
	m.metrics["lux_indexer_gas_used_24h"] = float64(counters.GasUsed24h)

	// Gas price metrics
	if gasPrices.Slow != nil {
		m.metrics["lux_indexer_gas_price_slow_gwei"] = gasPrices.Slow.Price
		m.metrics["lux_indexer_gas_time_slow_ms"] = gasPrices.Slow.Time
	}
	if gasPrices.Average != nil {
		m.metrics["lux_indexer_gas_price_average_gwei"] = gasPrices.Average.Price
		m.metrics["lux_indexer_gas_time_average_ms"] = gasPrices.Average.Time
	}
	if gasPrices.Fast != nil {
		m.metrics["lux_indexer_gas_price_fast_gwei"] = gasPrices.Fast.Price
		m.metrics["lux_indexer_gas_time_fast_ms"] = gasPrices.Fast.Time
	}
	m.metrics["lux_indexer_gas_used_ratio"] = gasPrices.GasUsedRatio

	// Block time metrics
	m.metrics["lux_indexer_block_time_seconds"] = blockTime.AverageBlockTime.Seconds()
	m.metrics["lux_indexer_block_time_min_seconds"] = blockTime.MinBlockTime.Seconds()
	m.metrics["lux_indexer_block_time_max_seconds"] = blockTime.MaxBlockTime.Seconds()

	return nil
}

// ServeHTTP implements http.Handler for Prometheus metrics endpoint
func (m *MetricsExporter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")

	// Write metrics in Prometheus exposition format
	for name, value := range m.metrics {
		fmt.Fprintf(w, "%s %g\n", name, value)
	}

	// Add metadata metrics
	fmt.Fprintf(w, "lux_indexer_last_update_timestamp %d\n", time.Now().Unix())
}

// StartBackgroundUpdater starts a goroutine to periodically update metrics
func (m *MetricsExporter) StartBackgroundUpdater(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	go func() {
		// Initial update
		m.UpdateMetrics(ctx)

		for {
			select {
			case <-ctx.Done():
				ticker.Stop()
				return
			case <-ticker.C:
				m.UpdateMetrics(ctx)
			}
		}
	}()
}

// MetricDefinition describes a Prometheus metric
type MetricDefinition struct {
	Name string
	Type string // counter, gauge, histogram
	Help string
}

// GetMetricDefinitions returns all metric definitions
func GetMetricDefinitions() []MetricDefinition {
	return []MetricDefinition{
		// Counters
		{Name: "lux_indexer_blocks_total", Type: "counter", Help: "Total number of indexed blocks"},
		{Name: "lux_indexer_transactions_total", Type: "counter", Help: "Total number of indexed transactions"},
		{Name: "lux_indexer_addresses_total", Type: "counter", Help: "Total number of unique addresses"},
		{Name: "lux_indexer_contracts_total", Type: "counter", Help: "Total number of contract addresses"},
		{Name: "lux_indexer_tokens_total", Type: "counter", Help: "Total number of token contracts"},
		{Name: "lux_indexer_token_transfers_total", Type: "counter", Help: "Total number of token transfers"},
		{Name: "lux_indexer_verified_contracts_total", Type: "counter", Help: "Total number of verified contracts"},

		// Gauges
		{Name: "lux_indexer_tps_24h", Type: "gauge", Help: "Transactions per second over last 24 hours"},
		{Name: "lux_indexer_gas_used_24h", Type: "gauge", Help: "Total gas used in last 24 hours"},
		{Name: "lux_indexer_gas_price_slow_gwei", Type: "gauge", Help: "Slow gas price in gwei"},
		{Name: "lux_indexer_gas_price_average_gwei", Type: "gauge", Help: "Average gas price in gwei"},
		{Name: "lux_indexer_gas_price_fast_gwei", Type: "gauge", Help: "Fast gas price in gwei"},
		{Name: "lux_indexer_gas_time_slow_ms", Type: "gauge", Help: "Estimated confirmation time for slow gas price (ms)"},
		{Name: "lux_indexer_gas_time_average_ms", Type: "gauge", Help: "Estimated confirmation time for average gas price (ms)"},
		{Name: "lux_indexer_gas_time_fast_ms", Type: "gauge", Help: "Estimated confirmation time for fast gas price (ms)"},
		{Name: "lux_indexer_gas_used_ratio", Type: "gauge", Help: "Recent block gas usage ratio (0-1)"},
		{Name: "lux_indexer_block_time_seconds", Type: "gauge", Help: "Average block time in seconds"},
		{Name: "lux_indexer_block_time_min_seconds", Type: "gauge", Help: "Minimum observed block time in seconds"},
		{Name: "lux_indexer_block_time_max_seconds", Type: "gauge", Help: "Maximum observed block time in seconds"},
		{Name: "lux_indexer_last_update_timestamp", Type: "gauge", Help: "Unix timestamp of last metrics update"},
	}
}

// GetMetricsHelp returns Prometheus-formatted HELP and TYPE lines
func GetMetricsHelp() string {
	var output string
	for _, def := range GetMetricDefinitions() {
		output += fmt.Sprintf("# HELP %s %s\n", def.Name, def.Help)
		output += fmt.Sprintf("# TYPE %s %s\n", def.Name, def.Type)
	}
	return output
}
