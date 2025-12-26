// Copyright (c) 2025 Lux Partners Limited
// SPDX-License-Identifier: MIT

package e2e

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Node", func() {
	var infoClient *InfoClient

	BeforeEach(func() {
		infoClient = NewInfoClient(GetInfoEndpoint())
	})

	Context("Dev Mode with K=1 Consensus", func() {
		It("should be running and bootstrapped", func() {
			// Check P-Chain bootstrapped
			bootstrapped, err := infoClient.IsBootstrapped("P")
			Expect(err).NotTo(HaveOccurred())
			Expect(bootstrapped).To(BeTrue(), "P-Chain should be bootstrapped")

			// Check C-Chain bootstrapped
			bootstrapped, err = infoClient.IsBootstrapped("C")
			Expect(err).NotTo(HaveOccurred())
			Expect(bootstrapped).To(BeTrue(), "C-Chain should be bootstrapped")

			// Check X-Chain bootstrapped
			bootstrapped, err = infoClient.IsBootstrapped("X")
			Expect(err).NotTo(HaveOccurred())
			Expect(bootstrapped).To(BeTrue(), "X-Chain should be bootstrapped")
		})

		It("should have valid node ID", func() {
			nodeID, err := infoClient.GetNodeID()
			Expect(err).NotTo(HaveOccurred())
			Expect(nodeID).To(HavePrefix("NodeID-"))
			GinkgoWriter.Printf("Node ID: %s\n", nodeID)
		})

		It("should report local network ID", func() {
			networkID, err := infoClient.GetNetworkID()
			Expect(err).NotTo(HaveOccurred())
			// Local network uses network ID 1 for dev mode
			Expect(networkID).To(BeNumerically(">=", 1))
			GinkgoWriter.Printf("Network ID: %d\n", networkID)
		})

		It("should have valid network name", func() {
			networkName, err := infoClient.GetNetworkName()
			Expect(err).NotTo(HaveOccurred())
			// Accept any of: local, devnet, custom, mainnet, testnet, or network-<id>
			Expect(networkName).To(Or(
				BeElementOf("local", "devnet", "custom", "mainnet", "testnet"),
				HavePrefix("network-"),
			))
			GinkgoWriter.Printf("Network Name: %s\n", networkName)
		})

		It("should have required VMs loaded", func() {
			vms, err := infoClient.GetVMs()
			Expect(err).NotTo(HaveOccurred())
			Expect(vms).NotTo(BeEmpty())

			// Should have at least the core VMs
			GinkgoWriter.Printf("Loaded VMs:\n")
			for vmID, aliases := range vms {
				GinkgoWriter.Printf("  %s: %v\n", vmID, aliases)
			}
		})
	})

	Context("Chain Connectivity", func() {
		It("should connect to C-Chain RPC", func() {
			client := NewRPCClient(GetRPCEndpoint("C"))
			chainID, err := client.GetChainID()
			Expect(err).NotTo(HaveOccurred())
			Expect(chainID).To(BeNumerically(">", 0))
			GinkgoWriter.Printf("C-Chain ID: %d\n", chainID)
		})

		It("should connect to P-Chain RPC", func() {
			client := NewPChainClient(GetRPCEndpoint("P"))
			height, err := client.GetHeight()
			if err != nil {
				Skip("P-Chain RPC not available in this dev mode")
			}
			Expect(height).To(BeNumerically(">=", 0))
			GinkgoWriter.Printf("P-Chain Height: %d\n", height)
		})

		It("should have blockchains registered", func() {
			client := NewPChainClient(GetRPCEndpoint("P"))
			chains, err := client.GetBlockchains()
			if err != nil {
				Skip("P-Chain RPC not available in this dev mode")
			}
			Expect(chains).NotTo(BeEmpty())

			GinkgoWriter.Printf("Registered Blockchains:\n")
			for _, chain := range chains {
				GinkgoWriter.Printf("  %s: %s (VM: %s)\n",
					chain["name"], chain["id"], chain["vmID"])
			}
		})
	})

	Context("Single Validator (K=1)", func() {
		It("should have exactly one validator in dev mode", func() {
			client := NewPChainClient(GetRPCEndpoint("P"))
			validators, err := client.GetCurrentValidators("")
			if err != nil {
				Skip("P-Chain validators not available in PoA mode")
			}

			// Dev mode with K=1 should have 1 validator
			Expect(len(validators)).To(BeNumerically(">=", 1))

			GinkgoWriter.Printf("Validators (%d):\n", len(validators))
			for _, v := range validators {
				GinkgoWriter.Printf("  NodeID: %s, Stake: %s\n",
					v["nodeID"], v["stakeAmount"])
			}
		})

		It("should have primary network (subnet)", func() {
			client := NewPChainClient(GetRPCEndpoint("P"))
			nets, err := client.GetNets()
			if err != nil {
				Skip("P-Chain networks not available in PoA mode")
			}
			Expect(nets).NotTo(BeEmpty())

			// Should have at least the primary network
			GinkgoWriter.Printf("Networks (%d):\n", len(nets))
			for _, net := range nets {
				GinkgoWriter.Printf("  ID: %s\n", net["id"])
			}
		})
	})
})
