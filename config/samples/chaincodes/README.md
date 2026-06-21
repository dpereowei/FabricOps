# FabricOps Sample Chaincodes

These samples are intentionally small CCaaS contracts for exercising FabricOps lifecycle automation.

Implementations:

- `node_settlement`: Fabric Node contract API
- `go_settlement`: Fabric Go contract API
- `java_settlement`: Fabric Java contract API

Each implementation exposes the same transaction names:

- `initLedger`
- `createSettlement(id, debtor, creditor, amount, currency)`
- `readSettlement(id)`
- `markSettled(id)`
- `settlementExists(id)`
- `getAllSettlements`

Default images can be built with each directory's `build_and_push.sh`:

```bash
PUSH=true ./build_and_push.sh
```

Override `IMAGE` and `PLATFORM` when publishing to a different registry or architecture.

Default image names:

- `ghcr.io/dpereowei/fabricops-node-settlement:0.1.0`
- `ghcr.io/dpereowei/fabricops-go-settlement:0.1.0`
- `ghcr.io/dpereowei/fabricops-java-settlement:0.1.0`

At runtime, the chaincode containers expect Fabric CCaaS identity from `CHAINCODE_ID` or `CORE_CHAINCODE_ID_NAME`, and they listen on `CHAINCODE_SERVER_ADDRESS` with `0.0.0.0:7052` as the sample default.
