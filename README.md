## Go Canxium

Official Golang execution layer implementation of the Canxium protocol.

[![API Reference](
https://camo.githubusercontent.com/915b7be44ada53c290eb157634330494ebe3e30a/68747470733a2f2f676f646f632e6f72672f6769746875622e636f6d2f676f6c616e672f6764646f3f7374617475732e737667
)](https://pkg.go.dev/github.com/ethereum/go-ethereum?tab=doc)
[![Go Report Card](https://goreportcard.com/badge/github.com/ethereum/go-ethereum)](https://goreportcard.com/report/github.com/ethereum/go-ethereum)
[![Travis](https://travis-ci.com/ethereum/go-ethereum.svg?branch=master)](https://travis-ci.com/ethereum/go-ethereum)
[![Discord](https://img.shields.io/badge/discord-join%20chat-blue.svg)](https://discord.gg/nthXNEv)

## Building the source

For prerequisites and detailed build instructions please read the [Installation Instructions](https://geth.ethereum.org/docs/getting-started/installing-geth).

Building `canxium` requires both a Go (version 1.19 or later) and a C compiler. You can install
them using your favourite package manager. Once the dependencies are installed, run

```shell
make canxium
```

or, to build the full suite of utilities:

```shell
make all
```

`canxium` binary is availabled at `./build/bin/canxium`.

## Executables

The go-canxium project comes with several wrappers/executables found in the `cmd`
directory.

|  Command   | Description                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                        |
| :--------: | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **`canxium`** | Our main Canxium CLI client. It is the entry point into the Canxium network (main-, test- or private net), capable of running as a full node (default), archive node (retaining all historical state) or a light node (retrieving data live). It can be used by other processes as a gateway into the Canxium network via JSON RPC endpoints exposed on top of HTTP, WebSocket and/or IPC transports. `canxium --help` and the [CLI page](https://geth.ethereum.org/docs/fundamentals/command-line-options) for command line options. |
|   `clef`   | Stand-alone signing tool, which can be used as a backend signer for `canxium`.                                                                                                                                                                                                                                                                                                                                                                                                                                                        |
|  `devp2p`  | Utilities to interact with nodes on the networking layer, without running a full blockchain.                                                                                                                                                                                                                                                                                                                                                                                                                                       |
|  `abigen`  | Source code generator to convert Canxium contract definitions into easy-to-use, compile-time type-safe Go packages. It operates on plain [Canxium contract ABIs](https://docs.soliditylang.org/en/develop/abi-spec.html) with expanded functionality if the contract bytecode is also available. However, it also accepts Solidity source files, making development much more streamlined. Please see our [Native DApps](https://geth.ethereum.org/docs/developers/dapp-developer/native-bindings) page for details.                                  |
| `bootnode` | Stripped down version of our Canxium client implementation that only takes part in the network node discovery protocol, but does not run any of the higher level application protocols. It can be used as a lightweight bootstrap node to aid in finding peers in private networks.                                                                                                                                                                                                                                               |
|   `evm`    | Developer utility version of the EVM (Canxium Virtual Machine) that is capable of running bytecode snippets within a configurable environment and execution mode. Its purpose is to allow isolated, fine-grained debugging of EVM opcodes (e.g. `evm --code 60ff60ff --debug run`).                                                                                                                                                                                                                                               |
| `rlpdump`  | Developer utility tool to convert binary RLP ([Recursive Length Prefix](https://ethereum.org/en/developers/docs/data-structures-and-encoding/rlp)) dumps (data encoding used by the Canxium protocol both network as well as consensus wise) to user-friendlier hierarchical representation (e.g. `rlpdump --hex CE0183FFFFFFC4C304050583616263`).                                                                                                                                                                                |

## Running `canxium`

Going through all the possible command line flags is out of scope here (please consult our
[CLI Wiki page](https://geth.ethereum.org/docs/fundamentals/command-line-options)),
but we've enumerated a few common parameter combos to get you up to speed quickly
on how you can run your own `canxium` instance.

### Hardware Requirements

Minimum:

* CPU with 2+ cores
* 4GB RAM
* 100GB free storage space to sync the Mainnet
* 8 MBit/sec download Internet service

Recommended:

* Fast CPU with 4+ cores
* 16GB+ RAM
* High-performance SSD with at least 1TB of free space
* 25+ MBit/sec download Internet service

### Init Canxium node with genesis

Before run your own canxium node or miner, you have to init the genesis block or else you can't
connect to the network.

```shell
$ canxium --db.engine=pebble init ./genesis/mainnet.genesis.json
```

By default, canxium will create new folder under your home: `~/.canxium`

#### Starting up your member nodes

With the bootnode operational and externally reachable (you can try
`telnet <ip> <port>` to ensure it's indeed reachable), start every subsequent `canxium`
node pointed to the bootnode for peer discovery via the `--bootnodes` flag. It will
probably also be desirable to keep the data directory of your private network separated, so
do also specify a custom `--datadir` flag.

```shell
$ canxium --db.engine=pebble --bootnodes enode://314f1041da4b27f5e4c02b4eac52ca7bd2f025cb585490cb7032fdb08db737aa10d7d64a780db697643ece6027d3bc1a511696420e76192648c0d2d74d099c73@boot.canxium.net:30303 --ethstats <node_name>:canxium@stats.canxium.org
```

#### Running a canxium miner

In a private network setting a single CPU miner instance is more than enough for
practical purposes as it can produce a stable stream of blocks at the correct intervals
without needing heavy resources (consider running on a single thread, no need for multiple
ones either). To start a `canxium` instance for mining, run it with all your usual flags, extended
by:

```shell
./build/bin/calcium --http --http.api eth,net,web3 --ethstats <node_name>:canxium@stats.canxium.org --mine --miner.etherbase 0x0000000000000000000000000000000000000000 --miner.threads 1 --db.engine=pebble --bootnodes enode://314f1041da4b27f5e4c02b4eac52ca7bd2f025cb585490cb7032fdb08db737aa10d7d64a780db697643ece6027d3bc1a511696420e76192648c0d2d74d099c73@boot.canxium.net:30303
```

Which will start mining blocks and transactions on a single CPU thread, crediting all
proceedings to the account specified by `--miner.etherbase`. You can further tune the mining
by changing the default gas limit blocks converge to (`--miner.targetgaslimit`) and the price
transactions are accepted at (`--miner.gasprice`).

## Contribution

Thank you for considering helping out with the source code! We welcome contributions
from anyone on the internet, and are grateful for even the smallest of fixes!

If you'd like to contribute to go-ethereum, please fork, fix, commit and send a pull request
for the maintainers to review and merge into the main code base. If you wish to submit
more complex changes though, please check up with the core devs first on [our Discord Server](https://discord.gg/invite/nthXNEv)
to ensure those changes are in line with the general philosophy of the project and/or get
some early feedback which can make both your efforts much lighter as well as our review
and merge procedures quick and simple.

Please make sure your contributions adhere to our coding guidelines:

 * Code must adhere to the official Go [formatting](https://golang.org/doc/effective_go.html#formatting)
   guidelines (i.e. uses [gofmt](https://golang.org/cmd/gofmt/)).
 * Code must be documented adhering to the official Go [commentary](https://golang.org/doc/effective_go.html#commentary)
   guidelines.
 * Pull requests need to be based on and opened against the `master` branch.
 * Commit messages should be prefixed with the package(s) they modify.
   * E.g. "eth, rpc: make trace configs optional"

Please see the [Developers' Guide](https://geth.ethereum.org/docs/developers/geth-developer/dev-guide)
for more details on configuring your environment, managing project dependencies, and
testing procedures.

## License

The go-ethereum library (i.e. all code outside of the `cmd` directory) is licensed under the
[GNU Lesser General Public License v3.0](https://www.gnu.org/licenses/lgpl-3.0.en.html),
also included in our repository in the `COPYING.LESSER` file.

The go-ethereum binaries (i.e. all code inside of the `cmd` directory) are licensed under the
[GNU General Public License v3.0](https://www.gnu.org/licenses/gpl-3.0.en.html), also
included in our repository in the `COPYING` file.
