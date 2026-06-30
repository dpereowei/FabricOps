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

The sample `FabricNetwork` keeps `imagePullPolicy: IfNotPresent` so local images loaded into OrbStack, kind, or minikube can be used before the images are published to a registry.

## Invoke Smoke

After the sample `FabricNetwork` reaches `Ready=True`, run the Node settlement invoke smoke against the local OrbStack sample with:

```bash
config/samples/chaincodes/node_settlement/invoke_smoke.sh
```

The script verifies the shared CCaaS package template, creates a temporary Fabric tools Job in the BankA namespace, then invokes `createSettlement` and queries the created settlement back through both sample peers.

For the Go sample, override the transaction names used by the smoke because Go contract methods are exported:

```bash
CREATE_FUNCTION=CreateSettlement READ_FUNCTION=ReadSettlement \
  config/samples/chaincodes/node_settlement/invoke_smoke.sh
```

## Compatibility Baseline

The sample chaincode runtime dependencies intentionally mirror Fablo's sample chaincodes because CCaaS compatibility can fail at invoke time when the chaincode shim and contract API versions drift from the Fabric network/tooling baseline.

- Node: `fabric-contract-api@2.4.2` and `fabric-shim@2.4.2`
- Go: `github.com/hyperledger/fabric-contract-api-go@v1.2.2`
- Java: `org.hyperledger.fabric-chaincode-java:fabric-chaincode-shim:2.5.0`

When changing these versions, rebuild the image and run an invoke smoke test against a Fablo-compatible Fabric network before treating the sample as compatible.
