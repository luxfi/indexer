# LUX Indexer

A data processing pipeline for the [LUX network](https://lux.network).

## Features

- Maintains a persistent log of all consensus events and decisions made on the LUX network.
- An [API](https://docs.lux.network/build/tools/indexer) allowing easy exploration of the index.

## Prerequisite

https://docs.docker.com/engine/install/ubuntu/

https://docs.docker.com/compose/install/

## Quick Start with Standalone Mode on Testnet

The easiest way to get started is to try out the standalone mode.

```shell script
git clone https://github.com/luxdefi/indexer.git $GOPATH/github.com/luxdefi/indexer
cd $GOPATH/github.com/luxdefi/indexer
make dev_env_start
make standalone_run
```

## [Production Deployment](docs/deployment.md)
