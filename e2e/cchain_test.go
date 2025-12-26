// Copyright (c) 2025 Lux Partners Limited
// SPDX-License-Identifier: MIT

package e2e

import (
	"context"
	"math/big"
	"os"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/luxfi/indexer/evm"
	"github.com/luxfi/indexer/storage"
)

var _ = Describe("C-Chain", func() {
	var (
		client *RPCClient
		store  storage.Store
	)

	BeforeEach(func() {
		client = NewRPCClient(GetRPCEndpoint("C"))

		dataDir := os.Getenv("LUX_E2E_DATA_DIR")
		if dataDir == "" {
			dataDir = filepath.Join(os.TempDir(), "lux-e2e-cchain")
		}
		os.MkdirAll(dataDir, 0755)

		cfg := storage.Config{
			Backend: storage.BackendSQLite,
			URL:     filepath.Join(dataDir, "cchain_test.db"),
			DataDir: dataDir,
		}

		var err error
		store, err = storage.New(cfg)
		Expect(err).NotTo(HaveOccurred())

		ctx := context.Background()
		err = store.Init(ctx)
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		if store != nil {
			store.Close()
		}
	})

	Context("Basic Connectivity", func() {
		It("should get chain ID", func() {
			chainID, err := client.GetChainID()
			Expect(err).NotTo(HaveOccurred())
			Expect(chainID).To(BeNumerically(">", 0))
			GinkgoWriter.Printf("C-Chain ID: %d\n", chainID)
		})

		It("should get block number", func() {
			blockNum, err := client.GetBlockNumber()
			Expect(err).NotTo(HaveOccurred())
			GinkgoWriter.Printf("Latest block: %d\n", blockNum)
		})

		It("should get genesis block", func() {
			block, err := client.GetBlockByNumber(0, false)
			Expect(err).NotTo(HaveOccurred())
			Expect(block).NotTo(BeNil())
			Expect(block["number"]).To(Equal("0x0"))

			GinkgoWriter.Printf("Genesis hash: %s\n", block["hash"])
			GinkgoWriter.Printf("Genesis state root: %s\n", block["stateRoot"])
		})
	})

	Context("Block Indexing", func() {
		It("should fetch and process recent blocks", func() {
			blockNum, err := client.GetBlockNumber()
			Expect(err).NotTo(HaveOccurred())

			// Fetch last 5 blocks
			startBlock := uint64(0)
			if blockNum > 5 {
				startBlock = blockNum - 5
			}

			for height := startBlock; height <= blockNum; height++ {
				block, err := client.GetBlockByNumber(height, true)
				Expect(err).NotTo(HaveOccurred())

				GinkgoWriter.Printf("Block %d: %s (txs: %d)\n",
					height,
					block["hash"].(string)[:16]+"...",
					len(block["transactions"].([]interface{})))
			}
		})

		It("should parse block transactions", func() {
			blockNum, err := client.GetBlockNumber()
			Expect(err).NotTo(HaveOccurred())

			// Find a block with transactions
			for i := blockNum; i > 0 && i > blockNum-100; i-- {
				block, err := client.GetBlockByNumber(i, true)
				Expect(err).NotTo(HaveOccurred())

				txs := block["transactions"].([]interface{})
				if len(txs) > 0 {
					GinkgoWriter.Printf("Block %d has %d transactions\n", i, len(txs))

					// Parse first transaction
					tx := txs[0].(map[string]interface{})
					GinkgoWriter.Printf("  TX: %s\n", tx["hash"])
					GinkgoWriter.Printf("  From: %s\n", tx["from"])
					GinkgoWriter.Printf("  To: %v\n", tx["to"])
					break
				}
			}
		})
	})

	Context("Account Operations", func() {
		It("should get balance of genesis account", func() {
			// In dev mode, the first address from mnemonic should be funded
			testAddresses := []string{
				"0x8db97C7cEcE249c2b98bDC0226Cc4C2A57BF52FC", // Common dev mode address
			}

			for _, addr := range testAddresses {
				balance, err := client.GetBalance(addr)
				if err == nil && balance.Cmp(big.NewInt(0)) > 0 {
					GinkgoWriter.Printf("Address %s balance: %s\n", addr, balance.String())
				}
			}
		})

		It("should get transaction count (nonce)", func() {
			testAddr := "0x8db97C7cEcE249c2b98bDC0226Cc4C2A57BF52FC"
			nonce, err := client.GetTransactionCount(testAddr)
			if err == nil {
				GinkgoWriter.Printf("Address %s nonce: %d\n", testAddr, nonce)
			}
		})
	})

	Context("Log/Event Indexing", func() {
		It("should get logs from recent blocks", func() {
			blockNum, err := client.GetBlockNumber()
			Expect(err).NotTo(HaveOccurred())

			fromBlock := uint64(0)
			if blockNum > 100 {
				fromBlock = blockNum - 100
			}

			logs, err := client.GetLogs(fromBlock, blockNum, nil, nil)
			if err != nil {
				GinkgoWriter.Printf("GetLogs: %v (may be normal if no events)\n", err)
				return
			}

			GinkgoWriter.Printf("Found %d logs in blocks %d-%d\n",
				len(logs), fromBlock, blockNum)

			// Analyze log topics
			topicCounts := make(map[string]int)
			for _, log := range logs {
				topics := log["topics"].([]interface{})
				if len(topics) > 0 {
					topicCounts[topics[0].(string)]++
				}
			}

			for topic, count := range topicCounts {
				GinkgoWriter.Printf("  Topic %s: %d events\n", topic[:10]+"...", count)
			}
		})

		It("should filter logs by Transfer topic", func() {
			blockNum, err := client.GetBlockNumber()
			Expect(err).NotTo(HaveOccurred())

			fromBlock := uint64(0)
			if blockNum > 1000 {
				fromBlock = blockNum - 1000
			}

			// ERC20 Transfer event topic
			transferTopic := "0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef"

			logs, err := client.GetLogs(fromBlock, blockNum, nil, [][]string{{transferTopic}})
			if err != nil {
				GinkgoWriter.Printf("GetLogs: %v\n", err)
				return
			}

			GinkgoWriter.Printf("Found %d Transfer events\n", len(logs))
		})
	})

	Context("Gas and Fee Analysis", func() {
		It("should analyze gas usage across blocks", func() {
			blockNum, err := client.GetBlockNumber()
			Expect(err).NotTo(HaveOccurred())

			startBlock := uint64(0)
			if blockNum > 10 {
				startBlock = blockNum - 10
			}

			var totalGasUsed, totalGasLimit uint64
			blocksAnalyzed := 0

			for i := startBlock; i <= blockNum; i++ {
				block, err := client.GetBlockByNumber(i, false)
				if err != nil {
					continue
				}

				gasUsed := parseHexUint(block["gasUsed"].(string))
				gasLimit := parseHexUint(block["gasLimit"].(string))

				totalGasUsed += gasUsed
				totalGasLimit += gasLimit
				blocksAnalyzed++
			}

			if blocksAnalyzed > 0 {
				avgGasUsed := totalGasUsed / uint64(blocksAnalyzed)
				avgGasLimit := totalGasLimit / uint64(blocksAnalyzed)
				utilization := float64(totalGasUsed) / float64(totalGasLimit) * 100

				GinkgoWriter.Printf("Gas Analysis (%d blocks):\n", blocksAnalyzed)
				GinkgoWriter.Printf("  Avg Gas Used: %d\n", avgGasUsed)
				GinkgoWriter.Printf("  Avg Gas Limit: %d\n", avgGasLimit)
				GinkgoWriter.Printf("  Block Utilization: %.2f%%\n", utilization)
			}
		})

		It("should get base fee from recent blocks", func() {
			blockNum, err := client.GetBlockNumber()
			Expect(err).NotTo(HaveOccurred())

			block, err := client.GetBlockByNumber(blockNum, false)
			Expect(err).NotTo(HaveOccurred())

			if baseFee, ok := block["baseFeePerGas"].(string); ok {
				baseFeeGwei := new(big.Int)
				baseFeeGwei.SetString(baseFee[2:], 16)
				// Convert to Gwei
				gwei := new(big.Int).Div(baseFeeGwei, big.NewInt(1e9))
				GinkgoWriter.Printf("Current base fee: %s Gwei\n", gwei.String())
			}
		})
	})
})

var _ = Describe("C-Chain EVM Indexer Integration", Ordered, func() {
	var (
		client  *RPCClient
		indexer *evm.Indexer
		store   storage.Store
		ctx     context.Context
	)

	BeforeAll(func() {
		ctx = context.Background()
		client = NewRPCClient(GetRPCEndpoint("C"))

		dataDir := filepath.Join(os.TempDir(), "lux-e2e-evm-indexer")
		os.MkdirAll(dataDir, 0755)

		cfg := storage.Config{
			Backend: storage.BackendSQLite,
			URL:     filepath.Join(dataDir, "evm_indexer.db"),
			DataDir: dataDir,
		}

		var err error
		store, err = storage.New(cfg)
		Expect(err).NotTo(HaveOccurred())

		err = store.Init(ctx)
		Expect(err).NotTo(HaveOccurred())

		chainID, _ := client.GetChainID()

		indexer, err = evm.NewIndexer(evm.Config{
			ChainID:     int64(chainID),
			ChainName:   "C-Chain",
			RPCEndpoint: GetRPCEndpoint("C"),
		}, store)
		Expect(err).NotTo(HaveOccurred())
	})

	AfterAll(func() {
		if store != nil {
			store.Close()
		}
	})

	It("should initialize EVM schema", func() {
		err := indexer.Init(ctx)
		Expect(err).NotTo(HaveOccurred())

		// Verify tables exist
		rows, err := store.Query(ctx, `
			SELECT name FROM sqlite_master
			WHERE type='table' AND name LIKE 'evm_%'
		`)
		Expect(err).NotTo(HaveOccurred())
		Expect(rows).NotTo(BeEmpty())

		GinkgoWriter.Printf("Created EVM tables:\n")
		for _, row := range rows {
			GinkgoWriter.Printf("  %s\n", row["name"])
		}
	})

	It("should store blocks via raw storage", func() {
		blockNum, err := client.GetBlockNumber()
		Expect(err).NotTo(HaveOccurred())

		startBlock := uint64(0)
		endBlock := blockNum
		if endBlock > 10 {
			startBlock = endBlock - 10
		}

		indexed := 0
		for height := startBlock; height <= endBlock; height++ {
			block, err := client.GetBlockByNumber(height, true)
			if err != nil {
				GinkgoWriter.Printf("Failed to get block %d: %v\n", height, err)
				continue
			}

			// Get optional fields with safe type assertions
			nonce := ""
			if v, ok := block["nonce"].(string); ok {
				nonce = v
			}
			difficulty := ""
			if v, ok := block["difficulty"].(string); ok {
				difficulty = v
			}
			baseFee := ""
			if v, ok := block["baseFeePerGas"].(string); ok {
				baseFee = v
			}
			var size uint64
			if v, ok := block["size"].(string); ok {
				size = parseHexUint(v)
			}

			// Store block via raw SQL - match all columns in the schema
			err = store.Exec(ctx, `
				INSERT OR REPLACE INTO evm_blocks
				(id, number, hash, parent_hash, nonce, miner, difficulty, total_difficulty,
				 gas_limit, gas_used, timestamp, tx_count, base_fee, size, created_at)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			`,
				block["hash"],
				height,
				block["hash"],
				block["parentHash"],
				nonce,
				block["miner"],
				difficulty,
				"", // total_difficulty
				parseHexUint(block["gasLimit"].(string)),
				parseHexUint(block["gasUsed"].(string)),
				time.Unix(int64(parseHexUint(block["timestamp"].(string))), 0),
				len(block["transactions"].([]interface{})),
				baseFee,
				size,
				time.Now(),
			)
			if err != nil {
				GinkgoWriter.Printf("Failed to store block %d: %v\n", height, err)
				continue
			}
			indexed++
		}

		GinkgoWriter.Printf("Indexed %d blocks (from %d to %d)\n", indexed, startBlock, endBlock)
		// Genesis block (0) should be indexable even in dev mode
		Expect(indexed).To(BeNumerically(">=", 1), "At least genesis block should be indexed")
	})

	It("should query indexed blocks", func() {
		rows, err := store.Query(ctx, `
			SELECT number, hash, gas_used, tx_count, timestamp
			FROM evm_blocks
			ORDER BY number DESC
			LIMIT 5
		`)
		Expect(err).NotTo(HaveOccurred())

		GinkgoWriter.Printf("Recent indexed blocks:\n")
		for _, row := range rows {
			GinkgoWriter.Printf("  Block %v: hash=%v, gas=%v, txs=%v\n",
				row["number"], row["hash"], row["gas_used"], row["tx_count"])
		}
	})
})

// Helper functions

func parseHexUint(s string) uint64 {
	if s == "" {
		return 0
	}
	n := new(big.Int)
	if len(s) > 2 && s[:2] == "0x" {
		n.SetString(s[2:], 16)
	} else {
		n.SetString(s, 16)
	}
	return n.Uint64()
}
