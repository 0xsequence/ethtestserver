package ethtestserver

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/big"
	"os"
	"path"
	"sync"
	"sync/atomic"
	"time"

	"github.com/0xsequence/ethkit/ethartifact"
	"github.com/0xsequence/ethkit/go-ethereum/accounts/abi"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/consensus/beacon"
	"github.com/ethereum/go-ethereum/consensus/ethash"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/history"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/txpool"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/eth"
	"github.com/ethereum/go-ethereum/eth/catalyst"
	"github.com/ethereum/go-ethereum/eth/ethconfig"
	"github.com/ethereum/go-ethereum/eth/filters"
	"github.com/ethereum/go-ethereum/eth/tracers"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/ethdb/memorydb"
	"github.com/ethereum/go-ethereum/ethdb/pebble"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/node"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/ethereum/go-ethereum/triedb"
)

const (
	chainDataDir = "chaindata" // Default directory for chain data
)

type ETHContractCaller struct {
	Address common.Address // Address of the contract
	ABI     abi.ABI
}

type ETHTestServer struct {
	mu sync.Mutex

	config ETHTestServerConfig

	initialized bool // Whether the server has been initialized

	node      *node.Node
	ethereum  *eth.Ethereum
	artifacts *ethartifact.ContractRegistry // Registry of JSON artifacts for the test server

	engine consensus.Engine // Consensus engine used by the test server
	db     ethdb.Database   // Database used by the test server

	provider atomic.Value // Provider for the test server, initialized lazily

	beacon *catalyst.SimulatedBeacon
}

type ETHTestServerConfig struct {
	AutoMining bool          // Whether to enable mining on the test server
	MineRate   time.Duration // How often to mine a new block

	MaxBlockNum uint64 // Maximum block number for the test server, defaults to unlimited

	ChainID *big.Int // Chain ID for the test server
	DataDir string   // Directory to store the blockchain data
	DBMode  string   // Database mode for the test server (e.g., "memory", "disk")

	InitialSigners   []*Signer                   // Initial signers for the test server
	InitialBalances  map[common.Address]*big.Int // Initial account balances for the test server
	InitialContracts map[common.Address][]byte   // Deployed bytecode-only contracts
	InitialBlockNum  uint64                      // Initial block number for the test server
	InitialTimestamp uint64                      // Initial timestamp for the test server

	ReorgProbability float64 // Probability of a reorg occurring during mining
	ReorgDepthMin    int     // Minimum depth of a reorg to trigger
	ReorgDepthMax    int     // Maximum depth of a reorg to trigger

	Genesis       *core.Genesis     // Base genesis block for the test server
	NodeConfig    *node.Config      // Base configuration for the Geth node
	ServiceConfig *ethconfig.Config // Base configuration for the Ethereum service

	HTTPHost string // Host to bind the HTTP RPC server to
	HTTPPort int    // Port to bind the HTTP RPC server to

	// Load artifacts into the test server registry
	Artifacts []ethartifact.Artifact
}

// NewETHTestServer creates a new Ethereum test server with the given configuration.
func NewETHTestServer(config *ETHTestServerConfig) (*ETHTestServer, error) {
	if config == nil {
		config = &ETHTestServerConfig{}
	}

	initialized := false
	if config.DBMode != "memory" && config.DataDir == "" {
		config.DataDir = "ethereum" // Default data directory
	}

	if config.DBMode == "memory" {
		config.DataDir = ""
	} else {
		dataDirStat, err := os.Stat(config.DataDir)
		if err == nil && !dataDirStat.IsDir() {
			return nil, fmt.Errorf("data directory %s exists but is not a directory", config.DataDir)
		}

		initialized = dataDirStat != nil && dataDirStat.IsDir()
	}

	if config.MineRate == 0 {
		config.MineRate = 1 * time.Second // Default mine rate
	}

	var genesisPath string
	if config.DataDir != "" {
		genesisPath = path.Join(config.DataDir, "genesis.json")
	}

	if initialized {
		buf, err := os.ReadFile(genesisPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read genesis config from %s: %w", genesisPath, err)
		}

		if err := json.Unmarshal(buf, &config.Genesis); err != nil {
			return nil, fmt.Errorf("failed to unmarshal genesis config from %s: %w", genesisPath, err)
		}
	} else {
		if config.Genesis == nil {
			config.Genesis = &core.Genesis{}
		}

		// override genesis config with custom values
		if config.Genesis.Config == nil {
			config.Genesis.Config = params.AllDevChainProtocolChanges
		}

		if config.Genesis.GasLimit == 0 {
			config.Genesis.GasLimit = 5_000_000
		}

		if config.Genesis.BaseFee == nil {
			config.Genesis.BaseFee = big.NewInt(params.InitialBaseFee)
		}

		if config.Genesis.Difficulty == nil {
			config.Genesis.Difficulty = common.Big1 // Default difficulty for the genesis block
		}

		config.Genesis.Alloc = make(types.GenesisAlloc)
		for _, signer := range config.InitialSigners {
			addr := signer.Address()
			balance, ok := config.InitialBalances[addr]
			if !ok {
				continue // Skip if no balance is set for this address
			}
			config.Genesis.Alloc[addr] = types.Account{
				Balance: balance,
			}
		}

		config.Genesis.Timestamp = config.InitialTimestamp

		// persist genesis config to disk
		if err := os.MkdirAll(config.DataDir, 0755); err != nil {
			return nil, fmt.Errorf("failed to create data directory %s: %w", config.DataDir, err)
		}

		buf, err := json.MarshalIndent(config.Genesis, "", "  ")
		if err != nil {
			return nil, fmt.Errorf("failed to marshal genesis config: %w", err)
		}
		if err := os.WriteFile(genesisPath, buf, 0644); err != nil {
			return nil, fmt.Errorf("failed to write genesis config to %s: %w", genesisPath, err)
		}
	}

	if config.HTTPHost == "" {
		config.HTTPHost = "127.0.0.1"
	}

	if config.HTTPPort == 0 {
		config.HTTPPort = 0
	}

	if config.NodeConfig == nil {
		config.NodeConfig = &node.Config{}
	}

	// override node config with custom values
	config.NodeConfig.Name = "ethtestserver"
	config.NodeConfig.HTTPHost = config.HTTPHost
	config.NodeConfig.HTTPPort = config.HTTPPort
	config.NodeConfig.HTTPModules = []string{"eth", "net", "web3", "txpool"}
	config.NodeConfig.Logger = log.NewLogger(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	config.NodeConfig.DataDir = config.DataDir

	stack, err := node.New(config.NodeConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create Geth node: %w", err)
	}

	if config.ServiceConfig == nil {
		config.ServiceConfig = &ethconfig.Defaults
	}

	// override service config with custom values
	config.ServiceConfig.Genesis = config.Genesis
	config.ServiceConfig.SyncMode = ethconfig.FullSync
	config.ServiceConfig.HistoryMode = history.KeepAll
	config.ServiceConfig.FilterLogCacheSize = 1000

	if !initialized {
		db, err := openDatabase(config, stack, false)
		if err != nil {
			return nil, fmt.Errorf("failed to open chain database: %w", err)
		}

		triedb := triedb.NewDatabase(db, triedb.HashDefaults)

		_ = config.Genesis.MustCommit(db, triedb)

		if err := triedb.Close(); err != nil {
			return nil, fmt.Errorf("failed to close trie database: %w", err)
		}

		if err := db.Close(); err != nil {
			return nil, fmt.Errorf("failed to close database: %w", err)
		}
	}

	// initialize the main service
	service, err := eth.New(stack, config.ServiceConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to register Ethereum service: %w", err)
	}

	// enable the regular APIs
	stack.RegisterAPIs(tracers.APIs(service.APIBackend))

	// enable the logs API
	filterSystem := filters.NewFilterSystem(
		service.APIBackend,
		filters.Config{},
	)

	stack.RegisterAPIs([]rpc.API{{
		Namespace: "eth",
		Service: filters.NewFilterAPI(
			filterSystem,
		),
	}})

	// set up simulated beacon
	simBeacon, err := catalyst.NewSimulatedBeacon(0, common.Address{}, service)
	if err != nil {
		return nil, fmt.Errorf("failed to create simulated beacon: %w", err)
	}
	if err := simBeacon.Fork(service.BlockChain().GetCanonicalHash(0)); err != nil {
		return nil, fmt.Errorf("failed to set beacon fork: %w", err)
	}

	engine := beacon.New(ethash.NewFaker()) // Use a fake ethash engine for testing

	s := &ETHTestServer{
		config: *config,

		node:     stack,
		ethereum: service,
		beacon:   simBeacon,

		initialized: initialized,

		db:     service.ChainDb(),
		engine: engine,

		artifacts: ethartifact.NewContractRegistry(),
	}

	// load artifacts into registry
	if len(config.Artifacts) > 0 {
		for _, artifact := range config.Artifacts {
			if err := s.artifacts.Add(artifact); err != nil {
				return nil, fmt.Errorf("failed to add artifact %s to registry: %w", artifact.ContractName, err)
			}
		}
	}

	return s, nil
}

func makeKeyValueStore(config *ETHTestServerConfig, stack *node.Node, options *node.DatabaseOptions) (ethdb.KeyValueStore, error) {
	if stack == nil {
		return nil, fmt.Errorf("stack cannot be nil")
	}

	if options == nil {
		return nil, fmt.Errorf("options cannot be nil")
	}

	if config.DBMode == "memory" || config.DataDir == "" {
		// Use an in-memory database for testing, this database is always created
		return memorydb.New(), nil
	}

	kvdb, err := pebble.New(
		stack.ResolvePath(chainDataDir),
		options.Cache,
		options.Handles,
		options.MetricsNamespace,
		options.ReadOnly,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to open pebbe database: %w", err)
	}

	return kvdb, nil
}

func openDatabase(config *ETHTestServerConfig, stack *node.Node, readOnly bool) (ethdb.Database, error) {
	options := node.DatabaseOptions{
		ReadOnly:          readOnly,
		Cache:             32 * 1024 * 1024,
		Handles:           128,
		MetricsNamespace:  "eth/db/chaindata",
		AncientsDirectory: stack.ResolveAncient(chainDataDir, ""),
	}

	kvdb, err := makeKeyValueStore(config, stack, &options)
	if err != nil {
		return nil, fmt.Errorf("failed to create key-value store: %w", err)
	}

	opts := rawdb.OpenOptions{
		ReadOnly:         readOnly,
		Ancient:          options.AncientsDirectory,
		Era:              options.EraDirectory,
		MetricsNamespace: options.MetricsNamespace,
	}

	db, err := rawdb.Open(kvdb, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to open raw database: %w", err)
	}

	return db, nil
}

func (s *ETHTestServer) Run(ctx context.Context) error {
	if s.node == nil {
		return fmt.Errorf("ETHTestServer: node is not initialized")
	}

	if s.ethereum == nil {
		return fmt.Errorf("ETHTestServer: ethereum service is not initialized")
	}

	if !s.initialized {
		// add first empty block to initialize the blockchain
		if _, _, err := s.GenBlocks(1, func(i int, gen *core.BlockGen) {}); err != nil {
			return fmt.Errorf("ETHTestServer: failed to generate initial block: %w", err)
		}
	}

	// Start HTTP server
	err := s.node.Start()
	if err != nil {
		return fmt.Errorf("ETHTestServer: failed to start Geth node: %w", err)
	}

	go func() {
		if !s.config.AutoMining {
			slog.Info("AutoMining is disabled, skipping block generation")
			return
		}

		ticker := time.NewTicker(s.config.MineRate)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				slog.Info("Stopping mining due to context cancellation")
				return
			case <-ticker.C:
				err := s.mineBlock()
				if err != nil {
					slog.Error("Failed to mine block", "error", err)
				}
			}
		}
	}()

	return nil
}

func (s *ETHTestServer) Commit() common.Hash {
	return s.beacon.Commit()
}

func (s *ETHTestServer) Fork(parentHash common.Hash) error {
	return s.beacon.Fork(parentHash)
}

func (s *ETHTestServer) Rollback() {
	s.beacon.Rollback()
}

func (s *ETHTestServer) Stop(ctx context.Context) error {
	if s.node != nil {
		err := s.node.Close()
		if err != nil {
			slog.Error("Failed to close Geth node", "error", err)
		}
		s.node = nil
	}

	if s.beacon != nil {
		err := s.beacon.Stop()
		if err != nil {
			slog.Error("Failed to stop simulated beacon", "error", err)
		}
		s.beacon = nil
	}

	return nil
}

func (s *ETHTestServer) waitForTxIndexing() error {
	bc := s.ethereum.BlockChain()

	// TODO: add max wait time
	for {
		progress, err := bc.TxIndexProgress()
		slog.Debug("Waiting for transaction indexing", "progress", progress, "error", err)
		if err == nil && progress.Done() {
			break // Transaction indexing is complete
		}

		// TODO: make this configurable
		time.Sleep(100 * time.Millisecond) // Wait for transaction indexing to complete
	}

	return nil
}

func (s *ETHTestServer) GenBlocks(n int, gen func(int, *core.BlockGen)) ([]*types.Block, []types.Receipts, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	bc := s.ethereum.BlockChain()

	latestBlockHeader := bc.CurrentBlock()
	latestBlock := bc.GetBlock(latestBlockHeader.Hash(), latestBlockHeader.Number.Uint64())

	if latestBlock.Number().Uint64() >= s.config.MaxBlockNum && s.config.MaxBlockNum > 0 {
		return nil, nil, fmt.Errorf("ETHTestServer: reached maximum block number %d", s.config.MaxBlockNum)
	}

	blocks, receipts := core.GenerateChain(
		s.config.Genesis.Config,
		latestBlock,
		s.engine,
		s.db,
		n,
		gen,
	)

	_, err := bc.InsertChain(blocks)
	if err != nil {
		return nil, nil, fmt.Errorf("ETHTestServer: failed to insert blocks into blockchain: %w", err)
	}

	if err := s.waitForTxIndexing(); err != nil {
		return nil, nil, fmt.Errorf("ETHTestServer: failed to wait for transaction indexing: %w", err)
	}

	return blocks, receipts, nil
}

func (s *ETHTestServer) HTTPEndpoint() string {
	if s.node == nil {
		return ""
	}

	return s.node.HTTPEndpoint()
}

func (s *ETHTestServer) DeployContract(ctx context.Context, signer *Signer, contractName string, constructorArgs ...interface{}) (*ETHContractCaller, error) {
	artifact, ok := s.artifacts.Get(contractName)
	if !ok {
		return nil, fmt.Errorf("contract %s not found in registry", contractName)
	}

	calldata := make([]byte, len(artifact.Bin))
	copy(calldata, artifact.Bin)

	var input []byte
	var err error
	if len(constructorArgs) > 0 && len(artifact.ABI.Constructor.Inputs) > 0 {
		input, err = artifact.ABI.Pack("", constructorArgs...)
	} else {
		input, err = artifact.ABI.Pack("")
	}
	if err != nil {
		return nil, fmt.Errorf("contract constructor pack failed: %w", err)
	}

	calldata = append(calldata, input...)

	_, receipts, err := s.GenBlocks(1, func(i int, gen *core.BlockGen) {
		nonce := gen.TxNonce(signer.Address())

		tx := types.NewContractCreation(
			nonce,
			new(big.Int),
			3_000_000, // Gas limit
			gen.BaseFee(),
			calldata, // Contract bytecode and constructor calldata
		)

		signedTxn, err := types.SignTx(tx, gen.Signer(), signer.RawPrivateKey())
		if err != nil {
			slog.Error("DeployContract: Failed to sign transaction", "error", err)
			return
		}

		gen.AddTx(signedTxn)
	})
	if err != nil {
		return nil, fmt.Errorf("failed to generate block for contract deployment: %w", err)
	}

	if len(receipts) == 0 || len(receipts[0]) == 0 {
		return nil, fmt.Errorf("no receipts found for contract deployment")
	}

	receipt := receipts[0][0]

	if receipt.Status != types.ReceiptStatusSuccessful {
		return nil, fmt.Errorf("contract deployment failed with status: %d", receipt.Status)
	}

	if receipt.ContractAddress == (common.Address{}) {
		return nil, fmt.Errorf("contract deployment failed, address is empty")
	}

	return &ETHContractCaller{
		Address: receipt.ContractAddress,
		ABI:     artifact.ABI,
	}, nil
}

func (s *ETHTestServer) ContractTransact(ctx context.Context, signer *Signer, contract *ETHContractCaller, methodName string, methodArgs ...interface{}) error {
	if signer == nil {
		return fmt.Errorf("signer cannot be nil")
	}

	if contract == nil {
		return fmt.Errorf("contract cannot be nil")
	}

	calldata, err := contract.ABI.Pack(methodName, methodArgs...)
	if err != nil {
		return fmt.Errorf("failed to pack contract method call: %w", err)
	}

	_, _, err = s.GenBlocks(1, func(i int, gen *core.BlockGen) {
		nonce := gen.TxNonce(signer.Address())

		tx := types.NewTransaction(
			nonce,
			common.Address(contract.Address),
			nil, // value
			300_000,
			gen.BaseFee(),
			calldata, // data
		)

		signedTxn, err := types.SignTx(tx, gen.Signer(), signer.RawPrivateKey())
		if err != nil {
			slog.Error("ContractTransact: Failed to sign transaction", "error", err)
			return
		}

		gen.AddTx(signedTxn)
	})
	if err != nil {
		return fmt.Errorf("failed to generate block for contract transaction: %w", err)
	}

	return nil
}

func (s *ETHTestServer) PrintStatus() {
	s.mu.Lock()
	defer s.mu.Unlock()

	bc := s.ethereum.BlockChain()
	slog.Info("ETHTestServer Status",
		"latestBlock", bc.CurrentBlock().Number,
		"latestBlockHash", bc.CurrentBlock().Hash().Hex(),
	)
}

// Mine triggers a block mining operation that will include all pending
// transactions in the transaction pool.
func (s *ETHTestServer) Mine() error {
	return s.mineBlock()
}

func (s *ETHTestServer) mineBlock() error {
	_, _, err := s.GenBlocks(1, func(i int, block *core.BlockGen) {
		latestBlockHeader := s.ethereum.BlockChain().CurrentBlock()

		txPool := s.ethereum.TxPool()
		pending := txPool.Pending(txpool.PendingFilter{})

		var gasUsed uint64 = 0

		blockGasLimit := latestBlockHeader.GasLimit

		// Choose as many transactions as possible without exceeding the parent's gas limit.
		var selectedTxs types.Transactions
		for _, batch := range pending {
			for _, lazy := range batch {
				if tx := lazy.Resolve(); tx != nil {
					txGas := tx.Gas()
					if gasUsed+txGas <= blockGasLimit {
						selectedTxs = append(selectedTxs, tx)
						gasUsed += txGas
					}
				}
			}
		}

		if len(selectedTxs) == 0 {
			//slog.Debug("No transactions selected for mining", "gasUsed", gasUsed, "blockGasLimit", blockGasLimit)
			return
		}

		for _, tx := range selectedTxs {
			//slog.Debug("Adding transaction to block", "txHash", tx.Hash().Hex(), "gasUsed", tx.Gas(), "blockGasLimit", blockGasLimit)
			block.AddTx(tx)
		}
	})

	if err != nil {
		return fmt.Errorf("failed to generate block: %w", err)
	}

	return nil
}
