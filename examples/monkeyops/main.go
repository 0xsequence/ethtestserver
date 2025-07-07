package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"log/slog"
	"math/big"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/0xsequence/ethkit/ethartifact"
	"github.com/0xsequence/ethtestserver"

	"github.com/ethereum/go-ethereum/common"
)

var (
	ERC20TestTokenArtifact   = ethtestserver.ERC20TestTokenArtifact
	ERC721TestTokenArtifact  = ethtestserver.ERC721TestTokenArtifact
	ERC1155TestTokenArtifact = ethtestserver.ERC1155TestTokenArtifact
)

func main() {
	var (
		runNative  = flag.Bool("run-native", false, "Run monkey transferors for native ETH transactions")
		runERC1155 = flag.Bool("run-erc1155", false, "Run monkey transferors for ERC1155 transactions")
		runERC20   = flag.Bool("run-erc20", false, "Run monkey transferors for ERC20 transactions")
		runERC721  = flag.Bool("run-erc721", false, "Run monkey transferors for ERC721 transactions")

		runAll   = flag.Bool("run-all", false, "Run all monkey transferors (native, ERC20, ERC721, ERC1155)")
		autoMine = flag.Bool("auto-mine", false, "Enable automatic mining of new blocks")
	)
	flag.Parse()

	var (
		wallet0 = ethtestserver.NewSignerWithKey("0x4a840bea3489bdebe9d90687b93e70e7b42341f96987382dd61fba2c6a976640") // 0x3c25c2353D0193625c868C4222C85592149E7f4B
		wallet1 = ethtestserver.NewSignerWithKey("0x035378650c1b589ee7811302365d5ec734f85baf94c6ce105a594d65513811c2") // 0x0E9d2aD0F0E906f5cC1385283C6aaA800Ee1c97e
		wallet2 = ethtestserver.NewSignerWithKey("0x4a42aa45eb965529b0c78c9ac9770e04bba024cd7ff3f08b5be52d1ccb8dd772") // 0xb2e7637Fc4b1fd1fAcF1E2E9aF2221fAe4d273D3
		wallet3 = ethtestserver.NewSignerWithKey("0x91a98fa4e3134f570adfa6ab1b6442e9b1111f9b10450d08c835207e363230c0") // 0x2735e4D29B9c6F6641E25CF60c12255B12EEec8e
		wallet4 = ethtestserver.NewSignerWithKey("0x2abb2f79ffcabc47f3aa1c038dc8dff4a459c69e4f4f392e6c35a412b87c3a18") // 0xe48151D11d6459A72677BeC02E1D0895F3973fE0
		wallet5 = ethtestserver.NewSignerWithKey("0xf4cccf93983a14c1f5dc1613558ea96460490f74cc8fe93d88aa4138fe4357db") // 0x25366930038CCD45610E8554Ad795A97EE63a9D5
		wallet6 = ethtestserver.NewSignerWithKey("0xbb40f29b98f4e5d2f94f3e530de6f47ac64e3dee3e2b0d49b6b496509bc8d716") // 0xa140Dcc8E04f96Fff324df1878e871e8A859aF87
		wallet7 = ethtestserver.NewSignerWithKey("0xcb510a76512cc7250406571ac21bd5c5b54f97c48bd851e5d21367a86a752287") // 0xf1C5D542e3224b9C858C621b4fA31347DE488F38
		wallet8 = ethtestserver.NewSignerWithKey("0x26b5fa333032217f5ab69b4c8df270eedaf98968daf8d2c139fec51f3d05c02e") // 0xE75d8245f878c4929A72f9429896b52c49baC625
		wallet9 = ethtestserver.NewSignerWithKey("0xc111b3ebb5bf8d838eca2210307ff93c3c9c7a21a24c458e837e2a228a64e900") // 0x184A0e08cD157Fb6cea13c0AdD643aCa6E7eEc24

		initialBalance = big.NewInt(1e18)
	)

	// list of known wallets
	knownWallets := []*ethtestserver.Signer{
		wallet0,
		wallet1,
		wallet2,
		wallet3,
		wallet4,
		wallet5,
		wallet6,
		wallet7,
		wallet8,
		wallet9,
	}

	config := &ethtestserver.ETHTestServerConfig{
		AutoMining:     *autoMine, // Use the autoMine flag to enable or disable automatic mining
		HTTPHost:       "localhost",
		HTTPPort:       8545,
		InitialSigners: knownWallets,

		ReorgProbability: 0.01, // 1% chance of reorgs
		ReorgDepthMin:    3,    // minimum depth for reorgs
		ReorgDepthMax:    6,    // maximum depth for reorgs

		DBMode: "disk",
		InitialBalances: map[common.Address]*big.Int{
			wallet0.Address(): initialBalance,
			wallet1.Address(): initialBalance,
			wallet2.Address(): initialBalance,
			wallet3.Address(): initialBalance,
			wallet4.Address(): initialBalance,
			wallet5.Address(): initialBalance,
			wallet6.Address(): initialBalance,
			wallet7.Address(): initialBalance,
			wallet8.Address(): initialBalance,
			wallet9.Address(): initialBalance,
		},
		Artifacts: []ethartifact.Artifact{
			ERC20TestTokenArtifact,
			ERC721TestTokenArtifact,
			ERC1155TestTokenArtifact,
		},
	}

	server, err := ethtestserver.NewETHTestServer(config)
	if err != nil {
		slog.Error("Failed to create test server", "error", err)
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Trap SIGINT and SIGTERM and cancel the context when received.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		slog.Info("Received shutdown signal, initiating graceful shutdown...")
		cancel()
	}()

	err = server.Run(ctx)
	if err != nil {
		log.Fatalf("Failed to run test server: %v", err)
	}

	slog.Info("ETH Test server started successfully", "endpoint", server.HTTPEndpoint())
	go printStatus(ctx, server)

	if *runNative || *runAll {
		// Run monkey transferors for native ETH
		go func() {
			monkeyTransferor, err := runMonkeyTransferors(ctx, server, knownWallets, knownWallets)
			if err != nil {
				slog.Error("Failed to run monkey transferors", "error", err)
				return
			}
			<-ctx.Done()
			monkeyTransferor.Stop(ctx)
		}()
	}

	if *runERC1155 || *runAll {
		// Run monkey transferors for ERC1155
		go func() {
			monkeyERC1155Transferor, err := runMonkeyERC1155Transferors(ctx, server, knownWallets, knownWallets, 1, 256, 1000)
			if err != nil {
				slog.Error("Failed to run monkey ERC1155 transferors", "error", err)
				return
			}
			<-ctx.Done()
			monkeyERC1155Transferor.Stop(ctx)
		}()
	}

	if *runERC20 || *runAll {
		// Run monkey transferors for ERC20
		go func() {
			monkeyERC20Transferor, err := runMonkeyERC20Transferors(ctx, server, knownWallets, knownWallets, 10_000)
			if err != nil {
				slog.Error("Failed to run monkey ERC20 transferors", "error", err)
				return
			}
			<-ctx.Done()
			monkeyERC20Transferor.Stop(ctx)
		}()
	}

	if *runERC721 || *runAll {
		// Run monkey transferors for ERC721
		go func() {
			monkeyERC721Transferor, err := runMonkeyERC721Transferors(ctx, server, knownWallets, knownWallets, 256)
			if err != nil {
				slog.Error("Failed to run monkey ERC721 transferors", "error", err)
				return
			}
			<-ctx.Done()
			monkeyERC721Transferor.Stop(ctx)
		}()
	}

	<-ctx.Done()
	slog.Info("Test run completed, stopping server")

	err = server.Stop(ctx)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return
		}
		slog.Error("Failed to stop test server", "error", err)
		os.Exit(1)
	}
}

func printStatus(ctx context.Context, server *ethtestserver.ETHTestServer) {
	for {
		select {
		case <-ctx.Done():
			slog.Info("Stopping status printing goroutine")
			return
		default:
		}
		server.PrintStatus()
		time.Sleep(10 * time.Second) // Print status every 10 seconds
	}
}
