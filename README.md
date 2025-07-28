# ETHTestServer

A Go package for creating local Ethereum test environments.

## Purpose

ETHTestServer provides a high-level API for spinning up a complete Ethereum
blockchain environment for testing, development, and experimentation.  It's
built on top of `go-ethereum` and designed to make complex blockchain testing
scenarios easy to implement.

## Features

- **Reorg Testing**: Simulate chain reorganizations to test application
  resilience
- **Custom Genesis**: Define initial blockchain state with custom accounts and
  contracts
- **Artifact Management**: Load and deploy contracts from JSON artifacts
- **State Persistence**: Maintain test state across server restarts
- **Runtime Controls**: Set mining limits by block count or duration

## Use Cases

- **Integration Testing**: Test services that interact with Ethereum networks
- **CI/CD**: Automated testing pipelines requiring blockchain environments

## Not Intended For

- **Production Use**: This package is not designed for production blockchain
  networks.
- **Contract Development**: While it supports contract deployment, it is not a
  substitute for dedicated contract development tools.
- **High-Performance Requirements**: It is optimized for testing scenarios, not
  for high-throughput applications.

## Quick Start

```go
import "github.com/0xsequence/ethtestserver"

// Create and start a test server
server, err := ethtestserver.NewETHTestServer(&ethtestserver.ETHTestServerConfig{
    AutoMining: true,
    MineRate:   2 * time.Second,
})
if err != nil {
    log.Fatal(err)
}

// Run the server
ctx := context.Background()
go server.Run(ctx)

// Your tests here...
// server.HTTPEndpoint() provides the RPC URL

// Cleanup
server.Stop(ctx)
```

## CLI Usage

`ETHTestServer` also includes a powerful command-line interface (CLI) that runs
the test server as a standalone process. This is ideal for when you don't want
to integrate the server directly into your Go test suite.

The CLI also includes "monkey" operators to automatically generate a variety of
transaction types for simulating blockchain noise.

### Installation

```
go install github.com/0xsequence/ethtestserver/cmd/ethtestserver
```

### Basic Usage

To start a default server with auto-mining enabled every second:

```sh
ethtestserver --auto-mine
```

The server will start and print its RPC endpoint (default:
`http://localhost:8545`).

#### 1. High-Load Stress Test

This command simulates a busy network with a high volume of diverse
transactions, running for 10,000 blocks.

```sh
ethtestserver \
  --auto-mine \
  --run-all \
  --min-transactions-per-block=50 \
  --max-transactions-per-block=100 \
  --max-blocks=10000 \
  --data-dir=./data-stress-test
```

#### 2. Deep Chain Reorganization Test

This command runs a simulation with a 10% chance of a very deep reorg (50-90
blocks) occurring after each block is mined.

```sh
ethtestserver \
  --auto-mine \
  --run-all \
  --max-blocks=10000 \
  --reorg-probability=0.1 \
  --reorg-depth-min=50 \
  --reorg-depth-max=90 \
  --data-dir=./data-reorg-test
```

#### 3. Maximum Throughput Benchmark

This command configures the server for maximum transaction throughput by
reducing mining and transaction generation intervals.

```sh
ethtestserver \
  --auto-mine \
  --run-all \
  --min-transactions-per-block=120 \
  --max-transactions-per-block=180 \
  --auto-mine-interval=1 \
  --monkey-mine-interval=1 \
  --max-blocks=50000 \
  --data-dir=./data-throughput-test
```

### All Options

For a full list of available flags and their descriptions, run:

```sh
ethtestserver --help
```

*ETHTestServer is designed for testing and development environments. It is not
intended for production blockchain networks.*

## License

[Apache License 2.0](https://www.apache.org/licenses/LICENSE-2.0)
