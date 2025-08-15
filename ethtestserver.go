package ethtestserver

import (
	"bytes"
	"context"
	"encoding/gob"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"math/rand"
	"os"
	"path"
	"sync"
	"sync/atomic"
	"time"

	"github.com/0xsequence/ethkit/ethartifact"
	"github.com/0xsequence/ethkit/go-ethereum/accounts/abi"

	"github.com/0xsequence/runnable"
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

	cockroachdbpebble "github.com/cockroachdb/pebble"
)

const (
	defaultDataDir = "data"

	chainDataDir = "chain" // Directory for chain data
	storeDataDir = "state" // Directory for the test server state

	maxMiningRetries = 5 // Maximum number of retries for mining a block
)

const (
	dbModeMemory = "memory"
	dbModeDisk   = "disk"
)

var (
	defaultLogLevel = slog.LevelDebug
)

var (
	ErrLimitReached = fmt.Errorf("ETHTestServer: limit reached, cannot generate more blocks")
)

// ETHContractCaller represents a deployed contract.
type ETHContractCaller struct {
	Address common.Address // Address of the contract
	ABI     abi.ABI
}

// ETHTestServerStats contains statistics about mining activity.
type ETHTestServerStats struct {
	BlocksMined      uint64        // Total number of blocks mined
	TxCount          uint64        // Total number of transactions processed
	CurrentBlock     uint64        // Current block number
	CurrentBlockHash common.Hash   // Hash of the current block
	BlocksPerSecond  float64       // number of blocks mined per second
	ElapsedTime      time.Duration // Total elapsed time since mining started
}

// ETHTestServerConfig represents the configuration for ETHTestServer.
type ETHTestServerConfig struct {
	AutoMining         bool          // Whether to enable mining on the test server
	AutoMiningInterval time.Duration // How often to mine a new block

	MaxBlockNum uint64        // Maximum block number for the test server (if nonzero)
	MaxDuration time.Duration // Maximum mining duration (if nonzero)

	ChainID *big.Int // Chain ID for the test server
	DataDir string   // Directory to store persistent data (chain data, state, genesis)
	DBMode  string   // Database mode ("memory" or "disk")

	InitialSigners   []*Signer                   // Initial signers for the test server
	InitialBalances  map[common.Address]*big.Int // Initial account balances
	InitialBlockNum  uint64                      // Initial block number
	InitialTimestamp uint64                      // Initial timestamp

	ReorgProbability float64 // Probability of a reorg occurring during mining
	ReorgDepthMin    int     // Minimum reorg depth
	ReorgDepthMax    int     // Maximum reorg depth

	Genesis       *core.Genesis     // Base genesis block for the test server
	NodeConfig    *node.Config      // Base configuration for the Geth node
	ServiceConfig *ethconfig.Config // Base configuration for the Ethereum service

	HTTPHost string // Host to bind the HTTP RPC server to
	HTTPPort int    // Port to bind the HTTP RPC server to

	WSHost string // Host to bind the WebSocket RPC server to
	WSPort int    // Port to bind the WebSocket RPC server to

	Artifacts []ethartifact.Artifact // JSON artifacts to load into the registry
}

// ETHTestServer contains the test server state and configuration.
type ETHTestServer struct {
	mu      sync.Mutex
	running atomic.Bool // Indicates if the server is currently running

	config    *ETHTestServerConfig
	stateDB   *cockroachdbpebble.DB // State database for the test server
	stateDBMu sync.Mutex            // Mutex to protect stateDB access

	initialized bool      // Whether the server has been initialized
	startTime   time.Time // Time when mining started

	node      *node.Node
	ethereum  *eth.Ethereum
	artifacts *ethartifact.ContractRegistry // Registry of JSON artifacts

	engine consensus.Engine // Consensus engine used by the test server
	db     ethdb.Database   // Database used by the test server

	beacon *catalyst.SimulatedBeacon

	stats ETHTestServerStats

	cancelFunc context.CancelFunc
	wg         sync.WaitGroup
}

// NewETHTestServer creates a new Ethereum test server with the given configuration.
func NewETHTestServer(config *ETHTestServerConfig) (*ETHTestServer, error) {
	if config == nil {
		config = &ETHTestServerConfig{}
	}

	s := &ETHTestServer{
		config: config,
	}

	// Default to disk mode if not specified or if the mode is invalid.
	if config.DBMode != dbModeMemory && config.DBMode != dbModeDisk {
		config.DBMode = dbModeDisk
	}

	if config.DataDir == "" {
		config.DataDir = defaultDataDir
	}

	// Validate reorg configuration.
	if config.ReorgProbability < 0 || config.ReorgProbability > 1 {
		return nil, fmt.Errorf("ReorgProbability must be between 0 and 1, got %f", config.ReorgProbability)
	}

	if config.ReorgProbability > 0 {
		if config.ReorgDepthMin <= 0 {
			return nil, fmt.Errorf("ReorgDepthMin must be >0 when ReorgProbability > 0, got %d", config.ReorgDepthMin)
		}
		if config.ReorgDepthMax < config.ReorgDepthMin {
			return nil, fmt.Errorf("ReorgDepthMax (got %d) must be >= ReorgDepthMin (%d)", config.ReorgDepthMax, config.ReorgDepthMin)
		}
	}

	// Ensure the data directory exists.
	if config.DBMode == dbModeDisk {
		if err := os.MkdirAll(config.DataDir, 0755); err != nil {
			return nil, fmt.Errorf("failed to create data directory %s: %w", config.DataDir, err)
		}
	}

	var err error
	s.stateDB, err = cockroachdbpebble.Open(
		s.resolvePath(storeDataDir),
		&cockroachdbpebble.Options{},
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create state store: %w", err)
	}

	// Set a default mine rate.
	if config.AutoMiningInterval == 0 {
		config.AutoMiningInterval = 1 * time.Second
	}

	if config.HTTPHost == "" {
		config.HTTPHost = "127.0.0.1"
	}
	if config.HTTPPort == 0 {
		config.HTTPPort = 0
	}
	if config.WSHost == "" {
		config.WSHost = "127.0.0.1"
	}
	if config.WSPort == 0 {
		config.WSPort = 0
	}
	if config.NodeConfig == nil {
		config.NodeConfig = &node.Config{}
	}

	// Override node config with custom values.
	config.NodeConfig.Name = "main"
	config.NodeConfig.DataDir = s.resolvePath(chainDataDir)
	config.NodeConfig.HTTPHost = config.HTTPHost
	config.NodeConfig.HTTPPort = config.HTTPPort
	config.NodeConfig.WSHost = config.WSHost
	config.NodeConfig.WSPort = config.WSPort
	config.NodeConfig.HTTPModules = []string{"eth", "net", "web3", "txpool"}
	config.NodeConfig.WSModules = []string{"eth", "net", "web3", "txpool"}
	config.NodeConfig.Logger = log.NewLogger(
		slog.NewJSONHandler(
			os.Stdout,
			&slog.HandlerOptions{
				Level: defaultLogLevel,
			},
		),
	)

	if config.ServiceConfig == nil {
		config.ServiceConfig = &ethconfig.Defaults
	}

	// Initialization steps: check if genesis has been created.
	initialized := false
	genesisPath := s.resolvePath("genesis.json")

	if config.DBMode == dbModeDisk {
		genesisStat, err := os.Stat(genesisPath)
		if err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("failed to check genesis config file %s: %w", genesisPath, err)
		}
		if err == nil && genesisStat.Size() > 0 {
			initialized = true
		}
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
		if config.Genesis.Config == nil {
			config.Genesis.Config = params.AllDevChainProtocolChanges
		}
		if config.Genesis.GasLimit == 0 {
			config.Genesis.GasLimit = 20_000_000
		}
		if config.Genesis.BaseFee == nil {
			config.Genesis.BaseFee = big.NewInt(params.InitialBaseFee)
		}
		if config.Genesis.Difficulty == nil {
			config.Genesis.Difficulty = common.Big1
		}

		config.Genesis.Alloc = make(types.GenesisAlloc)
		for _, signer := range config.InitialSigners {
			addr := signer.Address()
			balance, ok := config.InitialBalances[addr]
			if !ok {
				continue
			}
			config.Genesis.Alloc[addr] = types.Account{
				Balance: balance,
			}
		}

		config.Genesis.Timestamp = config.InitialTimestamp

		// Persist genesis config to disk.
		if config.DBMode == dbModeDisk {
			buf, err := json.MarshalIndent(config.Genesis, "", "  ")
			if err != nil {
				return nil, fmt.Errorf("failed to marshal genesis config: %w", err)
			}
			if err := os.WriteFile(genesisPath, buf, 0644); err != nil {
				return nil, fmt.Errorf("failed to write genesis config to %s: %w", genesisPath, err)
			}
		}
	}

	stack, err := node.New(config.NodeConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create Geth node: %w", err)
	}

	// Override service config.
	config.ServiceConfig.Genesis = config.Genesis
	config.ServiceConfig.SyncMode = ethconfig.FullSync
	config.ServiceConfig.HistoryMode = history.KeepAll
	config.ServiceConfig.FilterLogCacheSize = 5_000

	if !initialized {
		db, err := s.openDatabase(stack, false)
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

	service, err := eth.New(stack, config.ServiceConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to register Ethereum service: %w", err)
	}

	// Enable APIs.
	stack.RegisterAPIs(tracers.APIs(service.APIBackend))

	filterSystem := filters.NewFilterSystem(service.APIBackend, filters.Config{})
	stack.RegisterAPIs([]rpc.API{{
		Namespace: "eth",
		Service:   filters.NewFilterAPI(filterSystem),
	}})

	// Set up simulated beacon.
	simBeacon, err := catalyst.NewSimulatedBeacon(0, common.Address{}, service)
	if err != nil {
		return nil, fmt.Errorf("failed to create simulated beacon: %w", err)
	}

	bc := service.BlockChain()
	latestBlockHeader := bc.CurrentBlock()
	if err := simBeacon.Fork(latestBlockHeader.Hash()); err != nil {
		return nil, fmt.Errorf("failed to set beacon fork: %w", err)
	}

	engine := beacon.New(ethash.NewFaker())

	s.node = stack
	s.ethereum = service
	s.beacon = simBeacon
	s.initialized = initialized
	s.engine = engine
	s.db = service.ChainDb()
	s.artifacts = ethartifact.NewContractRegistry()

	// Load artifacts.
	if len(config.Artifacts) > 0 {
		for _, artifact := range config.Artifacts {
			if err := s.artifacts.Add(artifact); err != nil {
				return nil, fmt.Errorf("failed to add artifact %s to registry: %w", artifact.ContractName, err)
			}
		}
	}

	return s, nil
}

func (s *ETHTestServer) updateStatsAfterBlockGen(blocks ...*types.Block) {
	if len(blocks) == 0 {
		return
	}

	latestBlock := blocks[len(blocks)-1]

	for _, block := range blocks {
		s.stats.BlocksMined++
		s.stats.TxCount += uint64(len(block.Transactions()))
	}

	s.stats.CurrentBlock = latestBlock.Number().Uint64()
	s.stats.CurrentBlockHash = latestBlock.Hash()

	if s.startTime.IsZero() {
		s.stats.ElapsedTime = 0
		s.stats.BlocksPerSecond = 0
	} else {
		s.stats.ElapsedTime = time.Since(s.startTime)
		s.stats.BlocksPerSecond = float64(s.stats.BlocksMined) / s.stats.ElapsedTime.Seconds()
	}
}

// Stats returns a snapshot of the current mining statistics.
func (s *ETHTestServer) Stats() ETHTestServerStats {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stats
}

func (s *ETHTestServer) IsRunning() bool {
	if s == nil {
		return false
	}

	return s.running.Load()
}

// Run starts the ETHTestServer
func (s *ETHTestServer) Run(ctx context.Context) error {
	if err := s.validateRunPreconditions(); err != nil {
		return err
	}

	if err := s.node.Start(); err != nil {
		return fmt.Errorf("ETHTestServer: failed to start Geth node: %w", err)
	}

	if err := s.ensureChainInitialized(); err != nil {
		return err
	}

	// Wrap the incoming context with a cancellation function for internal use.
	ctx, cancel := context.WithCancel(ctx)
	s.cancelFunc = cancel
	s.startTime = time.Now()

	s.wg.Add(1)
	go s.runMainLoop(ctx)

	s.running.Store(true)

	return nil
}

func (s *ETHTestServer) validateRunPreconditions() error {
	if s == nil {
		return fmt.Errorf("ETHTestServer: nil server instance")
	}
	if s.running.Load() {
		return fmt.Errorf("ETHTestServer: server is already running")
	}
	if s.node == nil {
		return fmt.Errorf("ETHTestServer: node not initialized")
	}
	if s.ethereum == nil {
		return fmt.Errorf("ETHTestServer: ethereum service not initialized")
	}
	return nil
}

func (s *ETHTestServer) ensureChainInitialized() error {
	if s.initialized {
		return nil
	}

	if _, _, err := s.GenerateBlocks(1, nil); err != nil {
		return fmt.Errorf("ETHTestServer: failed to generate initial block: %w", err)
	}

	return nil
}

// runMainLoop runs the main block generation, and fork simulation loop.
func (s *ETHTestServer) runMainLoop(ctx context.Context) {
	defer s.wg.Done()

	s.running.Store(true)

	if !s.config.AutoMining {
		slog.Info("Auto mining is disabled, skipping main loop")
		return
	}

	ticker := time.NewTicker(s.config.AutoMiningInterval)
	defer ticker.Stop()

	var timeoutTimer *time.Timer
	var timeoutChan <-chan time.Time
	if s.config.MaxDuration > 0 {
		timeoutTimer = time.NewTimer(s.config.MaxDuration)
		timeoutChan = timeoutTimer.C
		defer timeoutTimer.Stop()
		slog.Info("Mining will stop after max duration", "maxDuration", s.config.MaxDuration,
			"stopTime", s.startTime.Add(s.config.MaxDuration).Format(time.RFC3339))
	}

	for {
		select {
		case <-ctx.Done():
			slog.Info("Stopping mining due to context cancellation")
			return

		case <-timeoutChan:
			elapsed := time.Since(s.startTime)
			slog.Info("Stopping mining: maximum duration reached", "maxDuration", s.config.MaxDuration, "elapsed", elapsed)
			return

		case <-ticker.C:
			err := s.nextTick()
			if err != nil {
				if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
					slog.Info("Stopping mining due to context error", "error", err)
					return
				}

				slog.Warn("Failed to execute mining tick", "error", err)
			}
		}
	}
}

// simulateFork simulates a blockchain fork at the given block number.
func (s *ETHTestServer) simulateFork(blockNumber int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.simulateForkUnlocked(blockNumber)
}

func (s *ETHTestServer) simulateForkUnlocked(blockNumber int) error {
	if blockNumber < 0 {
		return fmt.Errorf("blockNumber must be non-negative, got %d", blockNumber)
	}

	bc := s.ethereum.BlockChain()
	beforeFork := bc.CurrentBlock()
	slog.Info("Simulating fork", "currentBlockNumber", beforeFork.Number.Uint64(), "currentBlockHash", beforeFork.Hash().Hex())

	blockHash := bc.GetCanonicalHash(uint64(blockNumber))
	if err := s.beacon.Fork(blockHash); err != nil {
		return fmt.Errorf("failed to fork simulated beacon: %w", err)
	}

	s.beacon.Rollback()
	s.beacon.Commit()

	afterFork := bc.CurrentBlock()
	slog.Info("Fork simulated successfully", "newBlockNumber", afterFork.Number.Uint64(), "newBlockHash", afterFork.Hash().Hex())

	return nil
}

func (s *ETHTestServer) Stop(ctx context.Context) error {
	if s == nil {
		return fmt.Errorf("ETHTestServer: nil server instance")
	}

	if !s.running.CompareAndSwap(true, false) {
		return fmt.Errorf("ETHTestServer: server is not running")
	}

	// Cancel any background operations and wait for them to finish.
	if s.cancelFunc != nil {
		s.cancelFunc()
		s.cancelFunc = nil
	}

	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// ok
	case <-ctx.Done():
		return ctx.Err()
	}

	var errs []error

	s.stateDBMu.Lock()
	if s.stateDB != nil {
		if err := s.stateDB.Close(); err != nil {
			errs = append(errs, fmt.Errorf("failed to close state database: %w", err))
		}
		s.stateDB = nil
	}
	s.stateDBMu.Unlock()

	if s.beacon != nil {
		if err := s.beacon.Stop(); err != nil {
			errs = append(errs, fmt.Errorf("failed to stop simulated beacon: %w", err))
		}
		s.beacon = nil
	}

	if s.node != nil {
		if err := s.node.Close(); err != nil {
			errs = append(errs, fmt.Errorf("failed to close Geth node: %w", err))
		}
		s.node = nil
	}

	if len(errs) > 0 {
		return fmt.Errorf("errors during shutdown: %v", errs)
	}
	return nil
}

func (s *ETHTestServer) RetrieveValue(key string, value interface{}) (bool, error) {
	if !s.IsRunning() {
		return false, fmt.Errorf("ETHTestServer: server is not running")
	}
	s.stateDBMu.Lock()
	defer s.stateDBMu.Unlock()

	if s.stateDB == nil {
		return false, fmt.Errorf("state database is not initialized")
	}

	data, closer, err := s.stateDB.Get([]byte(key))
	if err != nil {
		if errors.Is(err, cockroachdbpebble.ErrNotFound) {
			return false, nil // Key not found
		}
		return false, fmt.Errorf("failed to get state for key %s: %w", key, err)
	}

	if err := closer.Close(); err != nil {
		return false, fmt.Errorf("failed to close state data for key %s: %w", key, err)
	}

	if len(data) == 0 {
		return false, nil // Key not found
	}

	if value == nil {
		return true, nil
	}

	decoder := gob.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(value); err != nil {
		return false, fmt.Errorf("failed to decode state data for key %s: %w", key, err)
	}

	return true, nil
}

func (s *ETHTestServer) StoreValue(key string, value interface{}) error {
	if !s.IsRunning() {
		return fmt.Errorf("ETHTestServer: server is not running")
	}
	if len(key) == 0 {
		return fmt.Errorf("key cannot be empty")
	}
	if value == nil {
		return fmt.Errorf("value cannot be nil")
	}

	var buf bytes.Buffer
	encoder := gob.NewEncoder(&buf)
	if err := encoder.Encode(value); err != nil {
		return fmt.Errorf("failed to encode value for key %s: %w", key, err)
	}

	s.stateDBMu.Lock()
	defer s.stateDBMu.Unlock()

	if s.stateDB == nil {
		return fmt.Errorf("state database is not initialized")
	}

	if err := s.stateDB.Set([]byte(key), buf.Bytes(), cockroachdbpebble.Sync); err != nil {
		return fmt.Errorf("failed to store state for key %s: %w", key, err)
	}

	return nil
}

func (s *ETHTestServer) DeleteValue(key string) error {
	if !s.IsRunning() {
		return fmt.Errorf("ETHTestServer: server is not running")
	}
	if len(key) == 0 {
		return fmt.Errorf("key cannot be empty")
	}

	s.stateDBMu.Lock()
	defer s.stateDBMu.Unlock()

	if s.stateDB == nil {
		return fmt.Errorf("state database is not initialized")
	}

	err := s.stateDB.Delete([]byte(key), cockroachdbpebble.Sync)
	if err != nil {
		return fmt.Errorf("failed to delete state for key %s: %w", string(key), err)
	}

	return nil
}

// GenerateBlocks generates n blocks using the provided block generation function.
func (s *ETHTestServer) GenerateBlocks(n int, blockGenFn func(int, *core.BlockGen) error) ([]*types.Block, []types.Receipts, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	currentBlock := s.ethereum.BlockChain().CurrentBlock()
	currentBlockNumber := currentBlock.Number.Uint64()

	if s.config.MaxBlockNum > 0 && currentBlockNumber >= s.config.MaxBlockNum {
		slog.Debug("ETHTestServer: maximum block number reached, cannot generate more blocks",
			"maxBlockNum", s.config.MaxBlockNum, "currentBlock", currentBlockNumber)
		return nil, nil, ErrLimitReached
	}

	return s.generateBlocksUnlocked(n, blockGenFn)
}

func (s *ETHTestServer) generateBlocksUnlocked(n int, blockGenFn func(int, *core.BlockGen) error) ([]*types.Block, []types.Receipts, error) {

	if n <= 0 {
		return nil, nil, fmt.Errorf("ETHTestServer: number of blocks to generate must be > 0")
	}

	bc := s.ethereum.BlockChain()
	latestBlockHeader := bc.CurrentBlock()
	latestBlock := bc.GetBlock(latestBlockHeader.Hash(), latestBlockHeader.Number.Uint64())

	var genErr error
	blocks, receipts := core.GenerateChain(
		s.config.Genesis.Config,
		latestBlock,
		s.engine,
		s.db,
		n,
		func(i int, gen *core.BlockGen) {
			if blockGenFn == nil || genErr != nil {
				return
			}
			if err := blockGenFn(i, gen); err != nil {
				genErr = fmt.Errorf("block generation failed: %w", err)
			}
		},
	)

	if genErr != nil {
		return nil, nil, fmt.Errorf("ETHTestServer: block generation error: %w", genErr)
	}

	_, err := bc.InsertChain(blocks)
	if err != nil {
		return nil, nil, fmt.Errorf("ETHTestServer: failed to insert blocks into blockchain: %w", err)
	}

	if err := s.waitForTxIndexing(context.Background()); err != nil {
		return nil, nil, fmt.Errorf("ETHTestServer: failed to wait for transaction indexing: %w", err)
	}

	s.updateStatsAfterBlockGen(blocks...)

	return blocks, receipts, nil
}

func (s *ETHTestServer) HTTPEndpoint() string {
	if !s.IsRunning() || s.node == nil {
		return ""
	}
	return s.node.HTTPEndpoint()
}

func (s *ETHTestServer) WSEndpoint() string {
	if !s.IsRunning() || s.node == nil {
		return ""
	}
	return s.node.WSEndpoint()
}

// DeployContract deploys a contract using the given signer and returns a caller.
func (s *ETHTestServer) DeployContract(signer *Signer, contractName string, constructorArgs ...interface{}) (*ETHContractCaller, error) {
	if !s.IsRunning() {
		return nil, fmt.Errorf("ETHTestServer: server is not running")
	}
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

	_, receipts, err := s.GenerateBlocks(1, func(i int, gen *core.BlockGen) error {
		nonce := gen.TxNonce(signer.Address())
		tx := types.NewContractCreation(
			nonce,
			new(big.Int),
			5_000_000, // Gas limit
			gen.BaseFee(),
			calldata,
		)

		signedTxn, err := types.SignTx(tx, gen.Signer(), signer.RawPrivateKey())
		if err != nil {
			return fmt.Errorf("failed to sign transaction: %w", err)
		}

		gen.AddTx(signedTxn)
		return nil
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

// ContractTransact calls a contract method using the given signer.
func (s *ETHTestServer) ContractTransact(signer *Signer, contract *ETHContractCaller, methodName string, methodArgs ...interface{}) error {
	if !s.IsRunning() {
		return fmt.Errorf("ETHTestServer: server is not running")
	}
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

	_, receipts, err := s.GenerateBlocks(1, func(i int, gen *core.BlockGen) error {
		nonce := gen.TxNonce(signer.Address())
		tx := types.NewTransaction(
			nonce,
			common.Address(contract.Address),
			nil, // value
			300_000,
			gen.BaseFee(),
			calldata,
		)

		signedTxn, err := types.SignTx(tx, gen.Signer(), signer.RawPrivateKey())
		if err != nil {
			return fmt.Errorf("failed to sign transaction: %w", err)
		}

		gen.AddTx(signedTxn)
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to generate block for contract transaction: %w", err)
	}

	if len(receipts) == 0 || len(receipts[0]) == 0 {
		return fmt.Errorf("no receipts found for contract transaction")
	}

	return nil
}

// PrintStatus logs the current status using our stats.
func (s *ETHTestServer) PrintStatus() {
	if !s.IsRunning() {
		slog.Warn("Cannot print status, server is not running")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	bc := s.ethereum.BlockChain()
	currentBlock := bc.CurrentBlock()
	currentBlockNumber := currentBlock.Number.Uint64()

	stats := s.stats

	logFields := []interface{}{
		"headBlock", currentBlockNumber,
		"currentBlock", stats.CurrentBlock,
		"blocksMined", stats.BlocksMined,
		"txCount", stats.TxCount,
		"blocksPerSecond", fmt.Sprintf("%.2f", stats.BlocksPerSecond),
		"elapsedTime", stats.ElapsedTime,
	}

	if s.config.MaxBlockNum > 0 {
		logFields = append(logFields, "maxBlockNum", s.config.MaxBlockNum)
		logFields = append(logFields, "blocksRemaining", s.config.MaxBlockNum-stats.CurrentBlock)
	}

	if s.config.MaxDuration > 0 && !s.startTime.IsZero() {
		elapsed := time.Since(s.startTime)
		remaining := s.config.MaxDuration - elapsed
		if remaining < 0 {
			remaining = 0
		}
		logFields = append(logFields, "maxDuration", s.config.MaxDuration)
		logFields = append(logFields, "elapsed", elapsed)
		logFields = append(logFields, "timeRemaining", remaining)
	}

	slog.Info("ETHTestServer Status", logFields...)
}

// Mine triggers a block mining operation.
func (s *ETHTestServer) Mine() error {
	if !s.IsRunning() {
		return fmt.Errorf("ETHTestServer: server is not running")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.maybeGenerateBlock()
}

// nextTick performs the next mining operation, generating a block and possibly
// simulating a fork.
func (s *ETHTestServer) nextTick() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	errs := []error{}

	// If conditions are met, generate a block.
	if err := s.maybeGenerateBlock(); err != nil {
		errs = append(errs, fmt.Errorf("failed to generate block: %w", err))
	}

	// If reorg is enabled, try to simulate a fork.
	if err := s.maybeSimulateFork(); err != nil {
		errs = append(errs, fmt.Errorf("failed to simulate fork: %w", err))
	}

	if len(errs) > 0 {
		return fmt.Errorf("ETHTestServer: errors during mining tick: %v", errors.Join(errs...))
	}

	return nil
}

func (s *ETHTestServer) maybeGenerateBlock() error {
	bc := s.ethereum.BlockChain()
	currentBlock := bc.CurrentBlock()
	currentBlockNumber := currentBlock.Number.Uint64()

	if s.config.MaxBlockNum > 0 && currentBlockNumber >= s.config.MaxBlockNum {
		slog.Debug("ETHTestServer: reached maximum block number", "maxBlockNum", s.config.MaxBlockNum, "currentBlock", currentBlockNumber)
		return nil // No more blocks to generate
	}

	if s.config.MaxDuration > 0 && !s.startTime.IsZero() {
		elapsed := time.Since(s.startTime)
		if elapsed >= s.config.MaxDuration {
			slog.Debug("ETHTestServer: reached maximum duration", "maxDuration", s.config.MaxDuration, "elapsed", elapsed)
			return nil // No more blocks to generate
		}
	}

	blocks, _, err := s.generateBlocksUnlocked(1, func(i int, gen *core.BlockGen) error {
		latestBlockHeader := s.ethereum.BlockChain().CurrentBlock()

		txPool := s.ethereum.TxPool()
		pending := txPool.Pending(txpool.PendingFilter{})
		var gasUsed uint64
		blockGasLimit := latestBlockHeader.GasLimit

		var selectedTxs types.Transactions
		for _, batch := range pending {
			for _, lazy := range batch {
				if tx := lazy.Resolve(); tx != nil {
					if gasUsed+tx.Gas() <= blockGasLimit {
						selectedTxs = append(selectedTxs, tx)
						gasUsed += tx.Gas()
					}
				}
			}
		}

		for _, tx := range selectedTxs {
			gen.AddTx(tx)
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to generate block: %w", err)
	}

	if len(blocks) > 0 {
		slog.Debug("ETHTestServer: latest block generated", "blockNumber", blocks[0].Number().Uint64(), "blockHash", blocks[0].Hash().Hex())
	}

	return nil
}

func (s *ETHTestServer) maybeSimulateFork() error {
	if s.config.ReorgProbability <= 0 {
		return nil // No reorg simulation needed
	}

	triggered := rand.Float64() < s.config.ReorgProbability
	if !triggered {
		return nil // No reorg simulation this time
	}

	bc := s.ethereum.BlockChain()
	currentHeight := int(bc.CurrentBlock().Number.Uint64())
	depthRange := s.config.ReorgDepthMax - s.config.ReorgDepthMin + 1

	var depth int
	if depthRange > 1 {
		depth = rand.Intn(depthRange) + s.config.ReorgDepthMin
	} else {
		depth = s.config.ReorgDepthMin
	}

	if depth >= currentHeight {
		depth = currentHeight - 1
	}

	if depth > 0 {
		slog.Info("Simulating fork", "chainHeight", currentHeight, "forkDepth", depth)
		if err := s.simulateForkUnlocked(currentHeight - depth); err != nil {
			return fmt.Errorf("failed to simulate fork: %w", err)
		}
		currentHeight = int(bc.CurrentBlock().Number.Uint64())
		slog.Info("Fork simulated successfully", "chainHeight", currentHeight, "forkDepth", depth)
	} else {
		slog.Info("Not enough blocks to simulate a fork", "chainHeight", currentHeight)
	}

	return nil
}

func (s *ETHTestServer) resolvePath(relPath ...string) string {
	resolved := path.Join(s.config.DataDir, path.Join(relPath...))
	log.Info("Resolved path", "path", resolved)
	return resolved
}

func (s *ETHTestServer) waitForTxIndexing(ctx context.Context) error {
	timeout := time.NewTimer(5 * time.Second)
	defer timeout.Stop()

	ticker := time.NewTicker(2 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()

		case <-timeout.C:
			return fmt.Errorf("timeout waiting for transaction indexing")

		case <-ticker.C:
			progress, err := s.ethereum.BlockChain().TxIndexProgress()
			if err == nil && progress.Done() {
				return nil
			}
		}
	}
}

func (s *ETHTestServer) makeKeyValueStore(stack *node.Node, options *node.DatabaseOptions) (ethdb.KeyValueStore, error) {
	if stack == nil {
		return nil, fmt.Errorf("stack cannot be nil")
	}
	if options == nil {
		return nil, fmt.Errorf("options cannot be nil")
	}

	if s.config.DBMode == dbModeMemory {
		return memorydb.New(), nil
	}

	kvdb, err := pebble.New(
		stack.ResolvePath("chaindata"),
		options.Cache,
		options.Handles,
		options.MetricsNamespace,
		options.ReadOnly,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to open pebble database: %w", err)
	}

	return kvdb, nil
}

func (s *ETHTestServer) openDatabase(stack *node.Node, readOnly bool) (ethdb.Database, error) {
	options := node.DatabaseOptions{
		ReadOnly:          readOnly,
		Cache:             512,
		Handles:           512,
		AncientsDirectory: stack.ResolveAncient("chaindata", ""),
	}

	kvdb, err := s.makeKeyValueStore(stack, &options)
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

var _ runnable.Runnable = (*ETHTestServer)(nil)
