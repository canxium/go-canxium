# KawPoW Integration for External Projects

This document explains how to use the go-canxium KawPoW implementation when importing it as a module in external projects.

## Build Tags Approach

The KawPoW package uses build tags to make it optional. By default, external projects importing go-canxium will **NOT** build the kawpow functionality, avoiding the need for C++ build dependencies.

## Requirements (Only if using KawPoW)

To use KawPoW functionality, you need:

1. **CMake** (version 3.5.1 or higher)
2. **Make**
3. **C++ compiler** (supporting C++11 standard)

## Usage for External Projects

### Option 1: Without KawPoW (Default)

```go
package main

import (
    "fmt"
    "github.com/ethereum/go-ethereum/core/raven" // This works fine
)

func main() {
    // Use other go-canxium functionality like Ravencoin cross-chain mining
    // kawpow.Hash() will panic if called without the kawpow build tag
}
```

### Option 2: With KawPoW (Requires Build Dependencies)

1. **Build the kawpow library**:
```bash
# First, build the kawpow library in the go-canxium source
cd path/to/go-canxium
make libkawpow
```

2. **Build your project with kawpow tag**:
```bash
go build -tags kawpow your-project
```

3. **Use kawpow in your code**:
```go
package main

import (
    "fmt"
    "github.com/ethereum/go-ethereum/crypto/kawpow"
)

func main() {
    headerHash := "7f5a0c4c6a8e0b6f1d2c3a4b5e6f7890abcdef1234567890abcdef1234567890"
    nonce := "1234567890abcdef"
    height := int64(100)
    
    hash, mixHash := kawpow.Hash(headerHash, nonce, height)
    
    fmt.Printf("Hash: %x\n", hash)
    fmt.Printf("Mix Hash: %x\n", mixHash)
}
```

## Cross-Chain Mining Support (Always Available)

The Ravencoin cross-chain mining functionality is always available without build tags:

```go
import "github.com/ethereum/go-ethereum/core/raven"

// Extract miner address from Ravencoin block
address, err := raven.GetMinerAddress(rvnBlock)

// Verify coinbase transaction
err = raven.VerifyCoinbase(rvnBlock, cauxAddress)
```

## Summary

- **Default import**: No KawPoW, no build dependencies required
- **With `-tags kawpow`**: Full KawPoW functionality, requires C++ build environment
- **Cross-chain mining**: Always available regardless of build tags
