package ethtestserver

import (
	"context"
	"fmt"
	"log/slog"
	"math/big"
	"math/rand"
	"sync/atomic"
	"time"

	"github.com/0xsequence/runnable"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/types"
)

type MonkeyNOPDoer struct{}

func (m *MonkeyNOPDoer) Do(ctx context.Context, op *MonkeyOperator, gen *core.BlockGen) (*types.Transaction, error) {
	// No operation, just a placeholder
	slog.Info("MonkeyNOPDoer: doing nothing")
	return nil, nil
}

type MonkeyOperator struct {
	config *MonkeyOperatorConfig // Configuration for the operator

	signers []*Signer
	done    chan struct{}

	running atomic.Bool // Flag to indicate if the operator is running

	doer MonkeyDoer

	blockAdder MonkeyBlockAdder
}

func (m *MonkeyOperator) PickSigners(n int) ([]*Signer, error) {
	// pick n non-duplicate signers from the list
	if n <= 0 || n > len(m.signers) {
		return nil, fmt.Errorf("invalid number of signers requested: %d, available: %d", n, len(m.signers))
	}

	pickedIndices := make(map[int]struct{})
	pickedSigners := make([]*Signer, 0, n)
	for len(pickedSigners) < n {
		index := rand.Intn(len(m.signers))
		if _, exists := pickedIndices[index]; !exists {
			pickedIndices[index] = struct{}{}
			pickedSigners = append(pickedSigners, m.signers[index])
		}
	}

	return pickedSigners, nil
}

type MonkeyDoer interface {
	Do(ctx context.Context, op *MonkeyOperator, gen *core.BlockGen) (*types.Transaction, error)
}

type MonkeyBlockAdder interface {
	GenBlocks(int, func(int, *core.BlockGen)) error
}

var (
	defaultMonkeyOperatorTickerInterval       = 100 * time.Millisecond // Default interval for executing operations
	defaultMonkeyOperatorTransactionsPerBlock = 5                      // Default transactions per block
)

type MonkeyOperatorConfig struct {
	Signers              []*Signer     // List of signers to use for operations
	Ticks                int           // Number of ticks to run, 0 means infinite
	TickerInterval       time.Duration // Interval for the ticker, default is 100ms
	TransactionsPerBlock int           // Number of transactions to add per block, default is 100
}

type MonkeyDoerFunc struct {
	doer func(ctx context.Context, op *MonkeyOperator, gen *core.BlockGen) (*types.Transaction, error)
}

func (f *MonkeyDoerFunc) Do(ctx context.Context, op *MonkeyOperator, gen *core.BlockGen) (*types.Transaction, error) {
	return f.doer(ctx, op, gen)
}

func NewMonkeyDoer(doer func(ctx context.Context, op *MonkeyOperator, gen *core.BlockGen) (*types.Transaction, error)) MonkeyDoer {
	return &MonkeyDoerFunc{doer: doer}
}

func NewMonkeyOperator(config *MonkeyOperatorConfig, monkeyDoer MonkeyDoer, blockAdder MonkeyBlockAdder) (*MonkeyOperator, error) {
	if blockAdder == nil {
		return nil, fmt.Errorf("blockAdder cannot be nil")
	}
	if config == nil {
		config = &MonkeyOperatorConfig{}
	}

	if config.TickerInterval <= 0 {
		config.TickerInterval = defaultMonkeyOperatorTickerInterval
	}

	if config.TransactionsPerBlock <= 0 {
		config.TransactionsPerBlock = defaultMonkeyOperatorTransactionsPerBlock
	}

	// copy signers to avoid modifying the original slice
	signers := make([]*Signer, len(config.Signers))
	copy(signers, config.Signers)

	return &MonkeyOperator{
		config:     config,
		signers:    signers,
		blockAdder: blockAdder,
		doer:       monkeyDoer,
		done:       make(chan struct{}),
	}, nil
}

func (m *MonkeyOperator) Run(ctx context.Context) error {
	if m.running.Load() {
		return fmt.Errorf("MonkeyOperator is already running")
	}

	ticker := time.NewTicker(m.config.TickerInterval)
	m.running.Store(true)

	go func() {
		defer ticker.Stop()

		var ticks int

		for {
			select {
			case <-ctx.Done():
				return
			case <-m.done:
				return
			case <-ticker.C:
				err := m.blockAdder.GenBlocks(1, func(i int, gen *core.BlockGen) {
					for j := 0; j < m.config.TransactionsPerBlock; j++ {
						tx, err := m.do(ctx, gen)
						if err != nil {
							slog.Error("MonkeyOperator: failed to do operation", "error", err)
							return
						}

						if tx != nil {
							gen.AddTx(tx)
							slog.Debug("MonkeyOperator: added transaction to block", "txHash", tx.Hash().Hex())
						} else {
							slog.Debug("MonkeyOperator: no transaction generated")
						}
					}
				})
				if err != nil {
					slog.Error("MonkeyOperator: failed to generate blocks", "error", err)
					continue
				}

				if ticks >= m.config.Ticks && m.config.Ticks > 0 {
					slog.Info("MonkeyOperator: reached configured ticks limit, stopping")
					m.running.Store(false)
					m.done <- struct{}{}
					return
				}

				ticks++
			}
		}
	}()

	return nil
}

func (m *MonkeyOperator) IsRunning() bool {
	if m == nil {
		return false
	}
	return m.running.Load()
}

func (m *MonkeyOperator) Stop(ctx context.Context) error {
	m.running.Store(false)

	m.done <- struct{}{}
	return nil
}

func (m *MonkeyOperator) do(ctx context.Context, gen *core.BlockGen) (*types.Transaction, error) {
	if m.doer == nil {
		return nil, fmt.Errorf("MonkeyOperator: doer is not set")
	}
	return m.doer.Do(ctx, m, gen)
}

var _ runnable.Runnable = (*MonkeyOperator)(nil)

func PickRandomSigner(signers []*Signer) *Signer {
	if len(signers) == 0 {
		panic("no signers available to pick from")
	}
	index := rand.Intn(len(signers))
	return signers[index]
}

func PickRandomAmount(a, b int64) *big.Int {
	if a > b {
		a, b = b, a
	}
	if a < 0 || b < 0 {
		panic("amounts must be non-negative")
	}
	if a == b {
		return big.NewInt(a)
	}
	return big.NewInt(a + rand.Int63n(b-a+1))
}
