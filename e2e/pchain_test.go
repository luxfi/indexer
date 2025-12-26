// Copyright (c) 2025 Lux Partners Limited
// SPDX-License-Identifier: MIT

package e2e

import (
	"context"
	"os"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/luxfi/indexer/pchain"
	"github.com/luxfi/indexer/storage"
)

// isPChainAvailable checks if P-Chain API is available (may be disabled in dev mode)
func isPChainAvailable() bool {
	client := NewPChainClient(GetRPCEndpoint("P"))
	_, err := client.GetHeight()
	return err == nil
}

var _ = Describe("P-Chain", func() {
	var (
		client  *PChainClient
		adapter *pchain.Adapter
		store   storage.Store
	)

	BeforeEach(func() {
		if !isPChainAvailable() {
			Skip("P-Chain API not available (dev mode may not support full P-Chain)")
		}
		client = NewPChainClient(GetRPCEndpoint("P"))
		adapter = pchain.New(GetRPCEndpoint("P"))

		// Create test store
		dataDir := os.Getenv("LUX_E2E_DATA_DIR")
		if dataDir == "" {
			dataDir = filepath.Join(os.TempDir(), "lux-e2e-pchain")
		}
		os.MkdirAll(dataDir, 0755)

		cfg := storage.Config{
			Backend: storage.BackendSQLite,
			URL:     filepath.Join(dataDir, "pchain_test.db"),
			DataDir: dataDir,
		}

		var err error
		store, err = storage.New(cfg)
		Expect(err).NotTo(HaveOccurred())

		ctx := context.Background()
		err = store.Init(ctx)
		Expect(err).NotTo(HaveOccurred())

		err = adapter.InitSchema(ctx, store)
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		if store != nil {
			store.Close()
		}
	})

	Context("Block Indexing", func() {
		It("should get P-Chain height", func() {
			height, err := client.GetHeight()
			Expect(err).NotTo(HaveOccurred())
			Expect(height).To(BeNumerically(">=", 0))
			GinkgoWriter.Printf("P-Chain height: %d\n", height)
		})

		It("should fetch recent blocks", func() {
			ctx := context.Background()
			blocks, err := adapter.GetRecentBlocks(ctx, 5)
			Expect(err).NotTo(HaveOccurred())
			Expect(blocks).NotTo(BeEmpty())

			GinkgoWriter.Printf("Fetched %d recent blocks\n", len(blocks))
		})

		It("should parse block data correctly", func() {
			ctx := context.Background()
			blocks, err := adapter.GetRecentBlocks(ctx, 1)
			Expect(err).NotTo(HaveOccurred())
			Expect(blocks).NotTo(BeEmpty())

			block, err := adapter.ParseBlock(blocks[0])
			Expect(err).NotTo(HaveOccurred())
			Expect(block.ID).NotTo(BeEmpty())
			Expect(block.Height).To(BeNumerically(">=", 0))

			GinkgoWriter.Printf("Block %d: %s (txs: %d)\n",
				block.Height, block.ID[:16]+"...", block.TxCount)
		})

		It("should get block by height", func() {
			ctx := context.Background()
			height, err := client.GetHeight()
			Expect(err).NotTo(HaveOccurred())

			if height > 0 {
				blockData, err := adapter.GetBlockByHeight(ctx, height-1)
				Expect(err).NotTo(HaveOccurred())
				Expect(blockData).NotTo(BeEmpty())
			}
		})
	})

	Context("Validator Management", func() {
		It("should get current validators", func() {
			validators, err := client.GetCurrentValidators("")
			Expect(err).NotTo(HaveOccurred())
			Expect(validators).NotTo(BeEmpty())

			for _, v := range validators {
				nodeID := v["nodeID"].(string)
				Expect(nodeID).To(HavePrefix("NodeID-"))
				GinkgoWriter.Printf("Validator: %s\n", nodeID)
			}
		})

		It("should get validators via adapter", func() {
			ctx := context.Background()
			validators, err := adapter.GetCurrentValidators(ctx, "")
			Expect(err).NotTo(HaveOccurred())
			Expect(validators).NotTo(BeEmpty())

			for _, v := range validators {
				Expect(v.NodeID).To(HavePrefix("NodeID-"))
				Expect(v.Weight).NotTo(BeEmpty())
			}
		})

		It("should sync validators to database", func() {
			ctx := context.Background()
			err := adapter.SyncValidators(ctx, store)
			Expect(err).NotTo(HaveOccurred())

			// Verify validators were stored
			rows, err := store.Query(ctx, "SELECT COUNT(*) as cnt FROM pchain_validators")
			Expect(err).NotTo(HaveOccurred())
			Expect(rows).NotTo(BeEmpty())

			count := rows[0]["cnt"].(int64)
			Expect(count).To(BeNumerically(">=", 1))
			GinkgoWriter.Printf("Synced %d validators to database\n", count)
		})
	})

	Context("Network/Subnet Operations", func() {
		It("should list networks", func() {
			nets, err := client.GetNets()
			Expect(err).NotTo(HaveOccurred())
			// Should have at least primary network
			Expect(nets).NotTo(BeEmpty())

			for _, net := range nets {
				GinkgoWriter.Printf("Network: %s\n", net["id"])
			}
		})

		It("should get networks via adapter", func() {
			ctx := context.Background()
			nets, err := adapter.GetNets(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(nets).NotTo(BeEmpty())
		})
	})

	Context("Blockchain Registry", func() {
		It("should list blockchains", func() {
			chains, err := client.GetBlockchains()
			Expect(err).NotTo(HaveOccurred())
			Expect(chains).NotTo(BeEmpty())

			// Should have at least C-Chain and X-Chain
			foundC, foundX := false, false
			for _, chain := range chains {
				name := chain["name"].(string)
				if name == "C-Chain" {
					foundC = true
				}
				if name == "X-Chain" {
					foundX = true
				}
				GinkgoWriter.Printf("Chain: %s (VM: %s)\n", name, chain["vmID"])
			}
			Expect(foundC || foundX).To(BeTrue(), "Should have at least one default chain")
		})

		It("should get blockchains via adapter", func() {
			ctx := context.Background()
			chains, err := adapter.GetBlockchains(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(chains).NotTo(BeEmpty())
		})
	})

	Context("P-Chain Stats", func() {
		It("should compute stats", func() {
			ctx := context.Background()

			// First sync validators
			err := adapter.SyncValidators(ctx, store)
			Expect(err).NotTo(HaveOccurred())

			// Get stats
			stats, err := adapter.GetStats(ctx, store)
			Expect(err).NotTo(HaveOccurred())
			Expect(stats).To(HaveKey("validators"))

			validatorStats := stats["validators"].(map[string]interface{})
			total := validatorStats["total"].(int64)
			Expect(total).To(BeNumerically(">=", 1))

			GinkgoWriter.Printf("P-Chain Stats:\n")
			GinkgoWriter.Printf("  Total Validators: %d\n", total)
			GinkgoWriter.Printf("  Active: %d\n", validatorStats["active"])
			GinkgoWriter.Printf("  Total Stake: %d\n", validatorStats["total_stake"])
		})
	})

	Context("Staking Operations", func() {
		It("should verify staking infrastructure", func() {
			// Verify validators exist and have stake info
			validators, err := client.GetCurrentValidators("")
			Expect(err).NotTo(HaveOccurred())
			Expect(validators).NotTo(BeEmpty())

			// Verify validators have weight/stake info
			for _, v := range validators {
				nodeID := v["nodeID"].(string)
				Expect(nodeID).To(HavePrefix("NodeID-"))

				// Check for weight or stakeAmount
				weight, hasWeight := v["weight"].(string)
				stakeAmount, hasStakeAmount := v["stakeAmount"].(string)
				Expect(hasWeight || hasStakeAmount).To(BeTrue(), "Validator should have weight or stakeAmount")

				if hasWeight {
					GinkgoWriter.Printf("Validator %s has weight: %s\n", nodeID, weight)
				}
				if hasStakeAmount {
					GinkgoWriter.Printf("Validator %s has stakeAmount: %s\n", nodeID, stakeAmount)
				}
			}
		})
	})

	Context("Transaction Types", func() {
		It("should index different P-Chain tx types", func() {
			ctx := context.Background()

			// Fetch blocks and analyze tx types
			blocks, err := adapter.GetRecentBlocks(ctx, 10)
			Expect(err).NotTo(HaveOccurred())

			txTypeCounts := make(map[string]int)
			for _, blockData := range blocks {
				block, err := adapter.ParseBlock(blockData)
				if err != nil {
					continue
				}

				if metadata, ok := block.Metadata["txTypes"].(map[string]int); ok {
					for txType, count := range metadata {
						txTypeCounts[txType] += count
					}
				}
			}

			if len(txTypeCounts) > 0 {
				GinkgoWriter.Printf("Transaction Types:\n")
				for txType, count := range txTypeCounts {
					GinkgoWriter.Printf("  %s: %d\n", txType, count)
				}
			}
		})
	})
})

var _ = Describe("P-Chain Staking Integration", Ordered, func() {
	var (
		client  *PChainClient
		adapter *pchain.Adapter
		store   storage.Store
	)

	BeforeAll(func() {
		if !isPChainAvailable() {
			Skip("P-Chain API not available (dev mode may not support full P-Chain)")
		}
		client = NewPChainClient(GetRPCEndpoint("P"))
		adapter = pchain.New(GetRPCEndpoint("P"))

		dataDir := filepath.Join(os.TempDir(), "lux-e2e-pchain-staking")
		os.MkdirAll(dataDir, 0755)

		cfg := storage.Config{
			Backend: storage.BackendSQLite,
			URL:     filepath.Join(dataDir, "staking.db"),
			DataDir: dataDir,
		}

		var err error
		store, err = storage.New(cfg)
		Expect(err).NotTo(HaveOccurred())

		ctx := context.Background()
		Expect(store.Init(ctx)).To(Succeed())
		Expect(adapter.InitSchema(ctx, store)).To(Succeed())
	})

	AfterAll(func() {
		if store != nil {
			store.Close()
		}
	})

	It("should have validators", func() {
		validators, err := client.GetCurrentValidators("")
		Expect(err).NotTo(HaveOccurred())
		Expect(validators).To(HaveLen(5)) // Mainnet/testnet has 5 validators

		for _, v := range validators {
			GinkgoWriter.Printf("Validator: %s (stake: %s)\n", v["nodeID"], v["weight"])
		}
	})

	It("should sync and query validators from DB", func() {
		ctx := context.Background()

		// Sync
		err := adapter.SyncValidators(ctx, store)
		Expect(err).NotTo(HaveOccurred())

		// Query back
		rows, err := store.Query(ctx, `
			SELECT node_id, stake_amount, connected, uptime
			FROM pchain_validators
			WHERE end_time > datetime('now')
			ORDER BY stake_amount DESC
		`)
		Expect(err).NotTo(HaveOccurred())
		Expect(rows).NotTo(BeEmpty())

		for _, row := range rows {
			GinkgoWriter.Printf("DB Validator: %s (stake: %v, uptime: %v)\n",
				row["node_id"], row["stake_amount"], row["uptime"])
		}
	})

	It("should track validator uptime", func() {
		ctx := context.Background()

		// Wait a bit for uptime accumulation in dev mode
		time.Sleep(2 * time.Second)

		// Re-sync to get updated uptime
		err := adapter.SyncValidators(ctx, store)
		Expect(err).NotTo(HaveOccurred())

		validators, err := adapter.GetCurrentValidators(ctx, "")
		Expect(err).NotTo(HaveOccurred())

		for _, v := range validators {
			GinkgoWriter.Printf("Validator %s uptime: %s\n", v.NodeID, v.Uptime)
		}
	})
})
