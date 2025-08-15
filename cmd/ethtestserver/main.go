package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"math/big"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/0xsequence/ethkit/ethartifact"
	"github.com/0xsequence/ethtestserver"
	"github.com/0xsequence/ethtestserver/operator/monkey"
	"github.com/ethereum/go-ethereum/common"
	"golang.org/x/sync/errgroup"
)

func main() {
	var (
		runNative    = flag.Bool("run-native", false, "Run monkey transferors for native ETH transactions")
		runERC1155   = flag.Bool("run-erc1155", false, "Run monkey transferors for ERC1155 transactions")
		runERC20     = flag.Bool("run-erc20", false, "Run monkey transferors for ERC20 transactions")
		runERC721    = flag.Bool("run-erc721", false, "Run monkey transferors for ERC721 transactions")
		runUniswapV2 = flag.Bool("run-uniswap-v2", false, "Run monkey transferors for Uniswap V2 transactions")

		runAll           = flag.Bool("run-all", false, "Run all monkey transferors (native, ERC20, ERC721, ERC1155)")
		autoMine         = flag.Bool("auto-mine", false, "Enable automatic mining of new blocks")
		autoMineInterval = flag.Int("auto-mine-interval", 1000, "Interval for automatic mining (milliseconds, default: 1s)")

		monkeyMineInterval = flag.Int("monkey-mine-interval", 1, "Interval for monkey mining operations (milliseconds, default: 1ms)")

		reorgProbability = flag.Float64("reorg-probability", 0.0, "Probability of a reorg occurring (default: disabled)")
		reorgDepthMin    = flag.Int("reorg-depth-min", 3, "Minimum depth for reorgs (default: 3)")
		reorgDepthMax    = flag.Int("reorg-depth-max", 6, "Maximum depth for reorgs (default: 6)")

		maxBlocks               = flag.Int("max-blocks", 1000, "Maximum number of blocks to mine before stopping (default: 1000)")
		minTransactionsPerBlock = flag.Int("min-transactions-per-block", 1, "Minimum number of transactions per block (default: 1)")
		maxTransactionsPerBlock = flag.Int("max-transactions-per-block", 10, "Maximum number of transactions per block (default: 10)")

		httpHost = flag.String("http-host", "localhost", "HTTP host for the test server (default: localhost)")
		httpPort = flag.Int("http-port", 8545, "HTTP port for the test server (default: 8545)")

		wsHost = flag.String("ws-host", "localhost", "WebSocket host for the test server (default: localhost)")
		wsPort = flag.Int("ws-port", 8546, "WebSocket port for the test server (default: 8546)")

		dataDir = flag.String("data-dir", "./data", "Data directory for the test server (default: ./data)")

		printHelp = flag.Bool("help", false, "Print help message and exit")
	)
	flag.Parse()

	if *printHelp {
		flag.Usage()
		return
	}

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

	initialBalances := make(map[common.Address]*big.Int)
	for _, w := range knownWallets {
		initialBalances[w.Address()] = initialBalance
	}

	config := &ethtestserver.ETHTestServerConfig{
		AutoMining:         *autoMine,
		AutoMiningInterval: time.Duration(*autoMineInterval) * time.Millisecond,
		HTTPHost:           *httpHost,
		HTTPPort:           *httpPort,
		WSHost:             *wsHost,
		WSPort:             *wsPort,
		DataDir:            *dataDir,
		MaxBlockNum:        uint64(*maxBlocks),
		ReorgProbability:   *reorgProbability,
		ReorgDepthMin:      *reorgDepthMin,
		ReorgDepthMax:      *reorgDepthMax,
		InitialSigners:     knownWallets,
		DBMode:             "disk",
		InitialBalances:    initialBalances,
		Artifacts: []ethartifact.Artifact{
			ethtestserver.ERC20TestTokenArtifact,
			ethtestserver.ERC721TestTokenArtifact,
			ethtestserver.ERC1155TestTokenArtifact,
			ethtestserver.WETH9Artifact,
			ethtestserver.UniswapV2FactoryArtifact,
			ethtestserver.UniswapV2Router02Artifact,
		},
	}

	monkeyOperatorConfig = monkey.MonkeyOperatorConfig{
		TickerInterval:          time.Duration(*monkeyMineInterval) * time.Millisecond,
		MinTransactionsPerBlock: *minTransactionsPerBlock,
		MaxTransactionsPerBlock: *maxTransactionsPerBlock,
	}

	server, err := ethtestserver.NewETHTestServer(config)
	if err != nil {
		slog.Error("Failed to create test server", "error", err)
		os.Exit(1)
	}

	g, ctx := errgroup.WithContext(context.Background())

	go printStatus(ctx, server)

	g.Go(func() error {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
		select {
		case <-sigCh:
			slog.Info("Received shutdown signal, initiating graceful shutdown...")
			return context.Canceled
		case <-ctx.Done():
			return ctx.Err()
		}
	})

	g.Go(func() error {
		if runErr := server.Run(ctx); runErr != nil {
			slog.Error("Failed to run ethtestserver", "error", runErr)
			return fmt.Errorf("failed to run ethtestserver: %w", runErr)
		}

		slog.Info("ethtestserver is running", "endpoint", server.HTTPEndpoint())
		return nil
	})

	// Wait for the server to start before proceeding with monkey transferors
	maxWaitingTime := time.NewTimer(30 * time.Second)
	defer maxWaitingTime.Stop()

	for !server.IsRunning() {
		select {
		case <-ctx.Done():
			slog.Error("Failed to start ethtestserver, exiting", "error", ctx.Err())
			os.Exit(1)
		case <-maxWaitingTime.C:
			slog.Error("Timeout waiting for ethtestserver to start")
			os.Exit(1)
		case <-time.After(100 * time.Millisecond):
			slog.Debug("Waiting for ethtestserver to start...")
		}
	}

	g.Go(func() error {
		<-ctx.Done()
		slog.Info("Stopping server...")

		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if stopErr := server.Stop(shutdownCtx); stopErr != nil && !errors.Is(stopErr, context.Canceled) {
			return fmt.Errorf("failed to stop server cleanly: %w", stopErr)
		}
		return nil
	})

	// Initialize monkey agents

	if *runNative || *runAll {
		g.Go(func() error {
			monkeyTransferor, err := runMonkeyTransferors(ctx, server, knownWallets, knownWallets)
			if err != nil {
				if errors.Is(err, ethtestserver.ErrLimitReached) {
					return nil
				}
				return fmt.Errorf("failed to run native monkey: %w", err)
			}
			<-ctx.Done()
			monkeyTransferor.Stop(context.Background())
			return nil
		})
	}

	if *runERC1155 || *runAll {
		g.Go(func() error {
			monkeyTransferor, err := runMonkeyERC1155Transferors(ctx, server, knownWallets, knownWallets, 1, 256, 1000)
			if err != nil {
				if errors.Is(err, ethtestserver.ErrLimitReached) {
					return nil
				}
				return fmt.Errorf("failed to run native monkey: %w", err)
			}
			<-ctx.Done()
			monkeyTransferor.Stop(context.Background())
			return nil
		})
	}

	if *runERC20 || *runAll {
		g.Go(func() error {
			monkeyTransferor, err := runMonkeyERC20Transferors(ctx, server, knownWallets, knownWallets, 10_000)
			if err != nil {
				if errors.Is(err, ethtestserver.ErrLimitReached) {
					return nil
				}
				return fmt.Errorf("failed to run native monkey: %w", err)
			}
			<-ctx.Done()
			monkeyTransferor.Stop(context.Background())
			return nil
		})
	}

	if *runERC721 || *runAll {
		g.Go(func() error {
			monkeyTransferor, err := runMonkeyERC721Transferors(ctx, server, knownWallets, knownWallets, 256)
			if err != nil {
				if errors.Is(err, ethtestserver.ErrLimitReached) {
					return nil
				}
				return fmt.Errorf("failed to run native monkey: %w", err)
			}
			<-ctx.Done()
			monkeyTransferor.Stop(context.Background())
			return nil
		})
	}

	if *runUniswapV2 || *runAll {
		g.Go(func() error {
			monkeyTrader, err := runMonkeyUniswapV2TradeGenerator(ctx, server, knownWallets)
			if err != nil {
				if errors.Is(err, ethtestserver.ErrLimitReached) {
					return nil
				}
				return fmt.Errorf("failed to run native monkey: %w", err)
			}
			<-ctx.Done()
			monkeyTrader.Stop(context.Background())
			return nil
		})
	}

	if err := g.Wait(); err != nil && !errors.Is(err, context.Canceled) {
		slog.Error("Task failed, shutting down", "error", err)
		os.Exit(1)
	}

	slog.Info("Simulation stopped")
}

func printStatus(ctx context.Context, server *ethtestserver.ETHTestServer) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			server.PrintStatus()
		}
	}
}
