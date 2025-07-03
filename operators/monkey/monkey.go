package monkey

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand"
	"sync/atomic"
	"time"

	"github.com/0xsequence/ethtestserver"
	"github.com/0xsequence/runnable"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/types"
)

var (
	defaultMonkeyOperatorTickerInterval       = 100 * time.Millisecond // Default interval for executing operations
	defaultMonkeyOperatorTransactionsPerBlock = 20                     // Default transactions per block
)

type MonkeyOperatorConfig struct {
	Signers              []*ethtestserver.Signer // List of signers to use for operations
	Ticks                int                     // Number of ticks to run, 0 means infinite
	TickerInterval       time.Duration           // Interval for the ticker, default is 100ms
	TransactionsPerBlock int                     // Number of transactions to add per block, default is 100
}

type MonkeyOperator struct {
	config *MonkeyOperatorConfig // Configuration for the operator

	signers []*ethtestserver.Signer
	done    chan struct{}

	running atomic.Bool // Flag to indicate if the operator is running

	doer MonkeyDoer

	blockGenerator MonkeyBlockGenerator
}

func (m *MonkeyOperator) PickSigners(n int) ([]*ethtestserver.Signer, error) {
	// pick n non-duplicate signers from the list
	if n <= 0 || n > len(m.signers) {
		return nil, fmt.Errorf("invalid number of signers requested: %d, available: %d", n, len(m.signers))
	}

	pickedIndices := make(map[int]struct{})
	pickedSigners := make([]*ethtestserver.Signer, 0, n)
	for len(pickedSigners) < n {
		index := rand.Intn(len(m.signers))
		if _, exists := pickedIndices[index]; !exists {
			pickedIndices[index] = struct{}{}
			pickedSigners = append(pickedSigners, m.signers[index])
		}
	}

	return pickedSigners, nil
}

func NewMonkeyOperator(config *MonkeyOperatorConfig, monkeyDoer MonkeyDoer, blockGenerator MonkeyBlockGenerator) (*MonkeyOperator, error) {
	if blockGenerator == nil {
		return nil, fmt.Errorf("blockGenerator cannot be nil")
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
	signers := make([]*ethtestserver.Signer, len(config.Signers))
	copy(signers, config.Signers)

	return &MonkeyOperator{
		config:         config,
		signers:        signers,
		blockGenerator: blockGenerator,
		doer:           monkeyDoer,
		done:           make(chan struct{}),
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
				_, _, err := m.blockGenerator.GenerateBlocks(1, func(i int, gen *core.BlockGen) error {
					for j := 0; j < m.config.TransactionsPerBlock; j++ {
						tx, err := m.do(ctx, gen)
						if err != nil {
							return fmt.Errorf("failed to generate transaction: %w", err)
						}

						if tx != nil {
							gen.AddTx(tx)
						}
					}

					return nil
				})

				if err != nil {
					slog.Error("MonkeyOperator: failed to generate blocks, stopping", "error", err)
					m.running.Store(false)
					m.done <- struct{}{}
					return
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
