package monkey

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	"github.com/0xsequence/ethtestserver/operator"
	"github.com/0xsequence/runnable"
	"github.com/ethereum/go-ethereum/core"
)

var (
	defaultMonkeyOperatorTickerInterval       = 1 * time.Second // Default interval for executing operations
	defaultMonkeyOperatorTransactionsPerBlock = 120             // Default transactions per block
)

type MonkeyOperatorConfig struct {
	Ticks                   int           // Number of ticks to run, 0 means infinite
	TickerInterval          time.Duration // Interval between operations
	TransactionsPerBlock    int           // Number of transactions to generate per block
	MinTransactionsPerBlock int           // Minimum number of transactions per block, if set to 0, it will use TransactionsPerBlock
	MaxTransactionsPerBlock int           // Maximum number of transactions per block, if set to 0, it will use TransactionsPerBlock
}

type MonkeyOperator struct {
	config   *MonkeyOperatorConfig         // Configuration for the operator
	txGen    operator.TransactionGenerator // Transaction generator to create transactions
	blockGen operator.BlockGenerator       // Block generator to create blocks

	running atomic.Bool // Indicates if the operator is running

	stopCh chan struct{}
	doneCh chan struct{}

	stopOnce sync.Once
}

func NewMonkeyOperator(blockGen operator.BlockGenerator, txGen operator.TransactionGenerator) (*MonkeyOperator, error) {
	return NewMonkeyOperatorWithConfig(&MonkeyOperatorConfig{}, blockGen, txGen)
}

func NewMonkeyOperatorWithConfig(config *MonkeyOperatorConfig, blockGen operator.BlockGenerator, txGen operator.TransactionGenerator) (*MonkeyOperator, error) {
	if txGen == nil {
		return nil, fmt.Errorf("MonkeyOperator: transaction generator is required")
	}

	if blockGen == nil {
		return nil, fmt.Errorf("MonkeyOperator: block generator is required")
	}

	if config == nil {
		return nil, fmt.Errorf("MonkeyOperator: configuration is required")
	}

	if config.TickerInterval <= 0 {
		config.TickerInterval = defaultMonkeyOperatorTickerInterval
	}

	if config.TransactionsPerBlock <= 0 {
		config.TransactionsPerBlock = defaultMonkeyOperatorTransactionsPerBlock
	}

	return &MonkeyOperator{
		config:   config,
		txGen:    txGen,
		blockGen: blockGen,
		stopCh:   make(chan struct{}),
		doneCh:   make(chan struct{}),
	}, nil
}

func (m *MonkeyOperator) Run(ctx context.Context) error {
	if m.running.Load() {
		return fmt.Errorf("MonkeyOperator is already running")
	}

	m.running.Store(true)

	if err := m.txGen.Initialize(ctx); err != nil {
		m.running.Store(false)
		return fmt.Errorf("MonkeyOperator: failed to initialize transaction generator: %w", err)
	}

	ticker := time.NewTicker(m.config.TickerInterval)

	go func() {
		defer func() {
			ticker.Stop()
			m.running.Store(false)

			close(m.doneCh)

			slog.Info("MonkeyOperator: stopped")
		}()

		var ticks int

		for {
			select {
			case <-ctx.Done():
				return

			case <-m.stopCh:
				return

			case <-ticker.C:
				_, _, err := m.blockGen.GenerateBlocks(1, func(_ int, gen *core.BlockGen) error {
					if gen == nil {
						// block without transactions
						return nil
					}

					transactionsPerBlock := m.config.TransactionsPerBlock
					if m.config.MinTransactionsPerBlock > 0 && m.config.MaxTransactionsPerBlock > 0 {
						transactionsPerBlock = m.config.MinTransactionsPerBlock + rand.Intn(m.config.MaxTransactionsPerBlock-m.config.MinTransactionsPerBlock+1)
					}

					for i := 0; i < transactionsPerBlock; i++ {
						tx, err := m.txGen.GenerateTransaction(ctx, gen)
						if err != nil {
							return fmt.Errorf("failed to generate transaction: %w", err)
						}

						if tx != nil {
							gen.AddTx(tx)
						}
					}

					return nil
				})

				ticks++

				if err != nil {
					slog.Error("MonkeyOperator: failed to generate blocks, stopping...", "error", err)
					return
				}

				// Check if the ticks limit has been reached (if non-zero)
				if m.config.Ticks > 0 && ticks >= m.config.Ticks {
					slog.Info("MonkeyOperator: reached configured ticks limit, stopping...")
					return
				}
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

// Stop signals the operator to stop and waits for it to finish gracefully.
func (m *MonkeyOperator) Stop(ctx context.Context) error {
	// Ensure that we only signal stop once.
	m.stopOnce.Do(func() {
		close(m.stopCh)
	})

	// Wait for the goroutine to finish or for the context to expire.
	select {
	case <-m.doneCh:
		return nil

	case <-ctx.Done():
		return ctx.Err()
	}
}

var _ runnable.Runnable = (*MonkeyOperator)(nil)
