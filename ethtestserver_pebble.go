//go:build pebble
// +build pebble

package ethtestserver

import (
	"fmt"
	"path"

	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/ethdb/memorydb"
	"github.com/ethereum/go-ethereum/ethdb/pebble"
	"github.com/ethereum/go-ethereum/node"
)

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
