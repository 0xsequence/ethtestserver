# ETHTestServer

A Go package for creating local Ethereum test environments.

## Purpose

ETHTestServer provides a high-level API for spinning up a complete Ethereum
blockchain environment for testing, development, and experimentation.  It's
built on top of `go-ethereum` and designed to make complex blockchain testing
scenarios easy to implement.

## Features

- **Reorg Testing**: Simulate chain reorganizations to test application resilience
- **Custom Genesis**: Define initial blockchain state with custom accounts and contracts
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

*ETHTestServer is designed for testing and development environments. It is not
intended for production blockchain networks.*

## License

[Apache License 2.0](https://www.apache.org/licenses/LICENSE-2.0)
