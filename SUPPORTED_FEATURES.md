# Supported features

This document tracks FabricOps feature support against the upstream
[hyperledger-labs/fablo](https://github.com/hyperledger-labs/fablo) feature
matrix. Fablo remains the compatibility benchmark because FabricOps should
eventually cover as much of that practical Fabric-network surface area as makes
sense on Kubernetes.

The `Fablo v2` and `Fablo v3` columns describe upstream Fablo support for
Hyperledger Fabric 2.5.x and 3.1.x. The `FabricOps status` column describes the
current operator implementation in this repository.

CI test links are intentionally empty for now. Fill them only after a feature
has repeatable CI or e2e coverage in this repository.

## Status legend

| Status | Meaning |
|--------|---------|
| Supported | Implemented in FabricOps and validated through envtest, OrbStack smoke, or both |
| Partial | Some support exists, but the behavior is incomplete or not broadly validated |
| Planned | Target feature, not implemented yet |
| Not applicable | Fablo feature does not map directly to the Kubernetes operator model |
| Not validated | Likely possible through configuration, but not verified yet |

## Version baseline

| Project | Fabric v2 baseline | Fabric v3 baseline | Notes |
|---------|--------------------|--------------------|-------|
| Fablo reference | 2.5.12 | 3.1.0 | Upstream Fablo supported-features baseline |
| FabricOps | Not validated yet | 3.1.0 | Current OrbStack sample smoke uses Fabric `3.1.0`; the operator accepts `spec.global.fabricVersion` |

## FabricOps operator features

| Feature | Fablo v2 | Fablo v3 | FabricOps status | Documented | CI tests | Notes |
|---------|----------|----------|------------------|------------|----------|-------|
| Declarative Kubernetes `FabricNetwork` CRD | n/a | n/a | Supported | README | | Main operator API at `fabricops.io/v1alpha1` |
| Per-org Kubernetes namespaces | n/a | n/a | Supported | README | | Uses compact `fo-<network>-<org>` names |
| Fabric CA deployment | n/a | n/a | Supported | README | | One CA Deployment per org |
| Fabric CA registrar bootstrap Secrets | n/a | n/a | Supported | README | | Deterministic bootstrap credential Secrets |
| Admin/orderer/peer registration and enrollment | n/a | n/a | Supported | README | | Fabric CA Jobs publish real MSP/TLS material into Secrets |
| MSP/TLS Secret validation | n/a | n/a | Supported | | | Invalid or missing material is surfaced in status |
| Persistent data for CAs/orderers/peers | n/a | n/a | Supported | README | | One PVC per Fabric component instance |
| Resource request/limit defaults | n/a | n/a | Supported | README | | Applies to Fabric workloads and helper Jobs |
| Status conditions | n/a | n/a | Supported | README | | `Ready`, `IdentityMaterialReady`, `ChannelsReady`, and `ObservabilityReady` |
| Kubernetes Events | n/a | n/a | Supported | | | Ready and failure transitions emit events |
| Finalizer cleanup | n/a | n/a | Supported | README | | Deletes owned org namespaces after ownership-label checks |
| Fabric operations endpoints | n/a | n/a | Supported | README | | Peer/orderer `/healthz` and `/metrics` Services |
| Prometheus `ServiceMonitor` output | n/a | n/a | Supported | README | | Opt-in via `spec.global.observability.serviceMonitor`; requires Prometheus Operator CRDs |
| TLS certificate rotation | n/a | n/a | Planned | | | Production hardening follow-up |
| Operations endpoint TLS | n/a | n/a | Planned | | | Local-dev path currently uses HTTP operations endpoints |
| NetworkPolicy generation | n/a | n/a | Supported | README, API | | Opt-in org-boundary policies via `spec.global.networkPolicy.enabled` |
| Packaged install bundle | n/a | n/a | Supported | README | | `dist/install.yaml` is generated from `config/default` and published with releases |
| Helm chart distribution | n/a | n/a | Supported | README | | Release artifacts include the generated `dist/chart` archive |

## Network configuration

| Feature | Fablo v2 | Fablo v3 | FabricOps status | Documented | CI tests | Notes |
|---------|----------|----------|------------------|------------|----------|-------|
| BFT consensus | n/a | yes | Planned | | | Target for Fabric v3 |
| RAFT consensus | yes | yes | Supported | README | | Current sample joins two orderers to a channel using etcdraft config |
| SOLO consensus | yes | n/a | Planned | | | Legacy Fabric v2 target only |
| TLS | yes | yes | Supported | README | | Workload TLS and orderer admin TLS are enabled when `spec.global.tls=true` |
| Orderer groups | no | no | Partial | README | | CRD models groups; one group is validated in the sample |
| Peer dev mode | yes | no | Planned | | | Kubernetes workflow still undecided |
| Peer DB - LevelDB | yes | yes | Supported | | | Default Fabric peer state database path; explicitly documented support still needed |
| Peer DB - CouchDB | yes | yes | Planned | | | `spec.orgs[].peer.db` exists but CouchDB sidecars/services are not wired yet |
| CA DB - SQLite | yes | yes | Supported | README | | Fabric CA default path used by current workloads |
| CA DB - Postgres | yes | yes | Planned | | | `spec.orgs[].ca.db` exists but external DB wiring is not implemented |
| CA DB - MySQL | yes | yes | Planned | | | External DB wiring is not implemented |
| Custom Fabric version | yes | yes | Partial | README | | `spec.global.fabricVersion` selects official Hyperledger image tags |
| Custom Fabric images | yes | yes | Partial | | | Arbitrary image repository overrides are not modelled yet |
| JSON/YAML config input | yes | yes | Supported | README | | Kubernetes CRs can be applied as YAML or JSON |

## Channels

| Feature | Fablo v2 | Fablo v3 | FabricOps status | Documented | CI tests | Notes |
|---------|----------|----------|------------------|------------|----------|-------|
| Channel config generation | yes | yes | Supported | README | | Generates `configtx.yaml` from `spec.channels` |
| Channel block generation | yes | yes | Supported | README | | Fabric tooling Job publishes channel block ConfigMaps |
| Orderer channel join | yes | yes | Supported | README | | Uses `osnadmin channel join` |
| Peer channel join | yes | yes | Supported | README | | Uses Fabric peer CLI Jobs per org namespace |
| Anchor peer updates | yes | yes | Supported | README | | Uses config fetch/patch/update flow |
| Channel readiness status | n/a | n/a | Supported | README | | `.status.channelStatus` and `ChannelsReady` |
| Multiple channels | yes | yes | Supported | samples | | The sample declares `settlement` and `audit`; kind e2e waits for all declared channels through `Ready=True` |
| Channel query scripts | yes | yes | Planned | | | Dedicated channel query helper scripts are not implemented yet |

## Chaincodes

| Feature | Fablo v2 | Fablo v3 | FabricOps status | Documented | CI tests | Notes |
|---------|----------|----------|------------------|------------|----------|-------|
| Node chaincode | yes | yes | Supported | README, samples | | Node CCaaS sample image is invoked successfully in kind e2e |
| Go chaincode | yes | yes | Supported | samples | | Go CCaaS sample image is invoked successfully in OrbStack |
| Java chaincode | yes | yes | Supported | samples | | Java CCaaS sample image is invoked successfully in OrbStack |
| Chaincode-as-a-Service (CCaaS) | yes | yes | Supported | README, samples | | Package metadata, install, approve, commit, and workloads are reconciled |
| CCaaS hot reload | yes | yes | Planned | | | Not modelled yet |
| Endorsement policies | yes | yes | Partial | samples | | `AND(...)` is validated by the multi-org sample; broader policy validation is still pending |
| Multi-org endorsements | yes | yes | Supported | samples | | Kind e2e invokes through BankA+BankB endorsement sets and queries both orgs |
| Private data collections | yes | yes | Supported | docs/private-data-collections.md, samples | | Kind e2e writes private data with transient input, confirms BankA can read it, confirms BankB cannot read the payload, and confirms BankB can query the private data hash |
| Chaincode scripts: invoke/query | yes | yes | Partial | samples | | Node/Go/Java invoke smoke exists; list/query utilities are not generalized yet |
| Chaincode scripts: list | yes | yes | Planned | | | Not implemented |
| Chaincode install/upgrade commands | yes | yes | Partial | | | Install/approve/commit supported; upgrade sequencing needs dedicated validation |
| Chaincode init-required lifecycle | yes | yes | Partial | API | | `initRequired` field exists; init flow is not smoke validated |

## Tools

| Feature | Fablo v2 | Fablo v3 | FabricOps status | Documented | CI tests | Notes |
|---------|----------|----------|------------------|------------|----------|-------|
| Fablo REST | yes | yes | Planned | | | Could become a Kubernetes-native gateway/helper workload later |
| Explorer | yes | no | Planned | | | Not implemented |
| Gateway client helper | n/a | n/a | Planned | | | Client material exists; gateway helper output is not implemented |
| Connection profiles | yes | yes | Planned | | | MSP/TLS and service DNS data exist, but profiles are not generated yet |
| Export network topology to Mermaid | yes | yes | Planned | | | Not implemented |
| Other `init` options | n/a | n/a | Planned | | | Not implemented |

## Fablo commands and FabricOps equivalents

| Fablo command feature | Fablo v2 | Fablo v3 | FabricOps status | Documented | CI tests | Notes |
|-----------------------|----------|----------|------------------|------------|----------|-------|
| `generate` | yes | yes | Partial | README | | Operator generates channel config, channel blocks, and CCaaS packages during reconciliation |
| `up` | yes | yes | Partial | README | | Local flow is `make install`, `make run`, `kubectl apply -k config/samples`; packaged install is pending |
| `start`, `stop`, `restart` | yes | yes | Planned | | | Workload lifecycle operations are not exposed as commands |
| `down`, `reset` | yes | yes | Partial | README | | Deleting a `FabricNetwork` cleans owned namespaces; reset semantics are not modelled |
| `prune`, `recreate` | yes | yes | Planned | | | Not implemented |
| `validate`, `extend-config` | yes | yes | Partial | API | | CRD schema gives basic validation; richer topology validation is pending |
| `version` | yes | yes | Planned | | | No FabricOps CLI version command yet |
| `init` (node, rest, dev) | yes | yes | Partial | samples | | Sample Node, Go, and Java chaincodes exist; no project generator command |
| `export-network-topology` to Mermaid | yes | yes | Planned | | | Not implemented |
| Other `init` options | n/a | n/a | Planned | | | Not implemented |

## Snapshot

| Feature | Fablo v2 | Fablo v3 | FabricOps status | Documented | CI tests | Notes |
|---------|----------|----------|------------------|------------|----------|-------|
| Create snapshot | yes | yes | Planned | | | Fabric snapshot operations are not automated yet |
| Restore snapshot | yes | yes | Planned | | | Restore workflow is not automated yet |

## Other features

| Feature | Fablo v2 | Fablo v3 | FabricOps status | Documented | CI tests | Notes |
|---------|----------|----------|------------------|------------|----------|-------|
| Peer dev mode | yes | no | Planned | | | Duplicated from Fablo's original matrix; kept as a target row |
| Hooks: post-generate | yes | yes | Planned | | | No hook system yet |
| Hooks: post-start | yes | yes | Planned | | | No hook system yet |
| Local Docker Compose network output | yes | yes | Not applicable | | | FabricOps targets Kubernetes resources instead |
| Terraform infrastructure examples | n/a | n/a | Supported | examples/terraform | | `examples/terraform/local-kind` provisions a local kind cluster for demos |
