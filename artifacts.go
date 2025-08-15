package ethtestserver

import (
	_ "embed"

	"github.com/0xsequence/ethkit/ethartifact"
)

var (
	//go:embed artifacts/ERC20TestToken.json
	artifact_ERC20TestToken string // OpenZeppelin ERC20 token for testing purposes

	//go:embed artifacts/ERC721TestToken.json
	artifact_ERC721TestToken string // OpenZeppelin ERC721 token for testing purposes

	//go:embed artifacts/ERC1155TestToken.json
	artifact_ERC1155TestToken string // OpenZeppelin ERC1155 token for testing purposes

	//go:embed artifacts/WETH9.json
	artifact_WETH9 string // WETH9 contract artifact for testing purposes

	//go:embed artifacts/UniswapV2/core/UniswapV2Factory.json
	artifact_UniswapV2Factory string // Uniswap V2 Factory contract artifact for testing purposes

	//go:embed artifacts/UniswapV2/periphery/UniswapV2Router02.json
	artifact_UniswapV2Router02 string // Uniswap V2 Router02 contract artifact for testing purposes

	//go:embed artifacts/UniswapV3/core/UniswapV3Factory.json
	artifact_UniswapV3Factory string // Uniswap V3 Factory contract artifact for testing purposes

	//go:embed artifacts/UniswapV3/periphery/NonfungiblePositionManager.json
	artifact_UniswapV3NonfungiblePositionManager string // Uniswap V3 NonfungiblePositionManager contract artifact for testing purposes

	//go:embed artifacts/UniswapV3/periphery/NonfungibleTokenPositionDescriptor.json
	artifact_UniswapV3NonfungibleTokenPositionDescriptor string // Uniswap V3 NonfungibleTokenPositionDescriptor contract artifact for testing purposes

	//go:embed artifacts/UniswapV3/core/SwapRouter.json
	artifact_UniswapV3SwapRouter string // Uniswap V3 SwapRouter contract artifact for testing purposes
)

var (
	ERC20TestTokenArtifact   = ethartifact.MustParseArtifactJSON(artifact_ERC20TestToken)
	ERC721TestTokenArtifact  = ethartifact.MustParseArtifactJSON(artifact_ERC721TestToken)
	ERC1155TestTokenArtifact = ethartifact.MustParseArtifactJSON(artifact_ERC1155TestToken)

	WETH9Artifact = ethartifact.MustParseArtifactJSON(artifact_WETH9)

	UniswapV2FactoryArtifact  = ethartifact.MustParseArtifactJSON(artifact_UniswapV2Factory)
	UniswapV2Router02Artifact = ethartifact.MustParseArtifactJSON(artifact_UniswapV2Router02)
	UniswapV3FactoryArtifact  = ethartifact.MustParseArtifactJSON(artifact_UniswapV3Factory)

	UniswapV3NonfungiblePositionManagerArtifact         = ethartifact.MustParseArtifactJSON(artifact_UniswapV3NonfungiblePositionManager)
	UniswapV3NonfungibleTokenPositionDescriptorArtifact = ethartifact.MustParseArtifactJSON(artifact_UniswapV3NonfungibleTokenPositionDescriptor)
	UniswapV3SwapRouterArtifact                         = ethartifact.MustParseArtifactJSON(artifact_UniswapV3SwapRouter)
)
