# Fabric and Fablo Parity Roadmap

This roadmap turns the post-demo direction into PR-sized work. It is intentionally separate from `SUPPORTED_FEATURES.md`: the support matrix records current capability, while this document explains how FabricOps should close the next gaps.

## Development Cadence

- Start every milestone or milestone slice from `main`.
- Use feature branches named `feature/<short-topic>`.
- Open a PR for every milestone slice. Large milestones should split into multiple PRs with their own validation proof.
- Keep each PR tied to one user-visible capability, compatibility gap, or design artifact.
- Run the lightest validation that proves the slice, then add kind e2e coverage when the slice changes real network behavior.
- Prepare releases after a coherent set of user-visible capabilities lands, not after every small internal cleanup.

## Reference Inputs

- FabricOps current API: `api/v1alpha1/fabricnetwork_types.go`
- FabricOps current capability table: `SUPPORTED_FEATURES.md`
- FabricOps sample network: `config/samples/fabricops_v1alpha1_fabricnetwork.yaml`
- Fablo supported features: `hyperledger-labs/fablo/SUPPORTED_FEATURES.md`
- Fablo comparison samples from `hyperledger-labs/fablo`:
  - `samples/fablo-config-hlf3-2orgs-1chaincode-raft-ccaas.json`
  - `samples/fablo-config-hlf2-2orgs-2chaincodes-private-data.yaml`
  - `samples/fablo-config-hlf3-bft-1orgs-1chaincode.json`
  - `samples/fablo-config-hlf2-1org-1chaincode-k8s.json`

## Current FabricOps Baseline

FabricOps can install an operator into Kubernetes, reconcile a multi-org Fabric network from a `FabricNetwork` resource, create per-org namespaces, enroll identities through Fabric CA, bootstrap channels, configure anchor peers, run CCaaS lifecycle install/approve/commit, deploy peer-specific CCaaS workloads, and prove invokes through multiple peers in kind e2e.

That is a strong Kubernetes foundation. The next work should avoid re-proving the same happy path and instead expand compatibility where Fabric users naturally expect depth.

## Parity Gap Map

| Area | Current FabricOps shape | Fablo reference shape | Next useful slice |
|------|-------------------------|-----------------------|-------------------|
| Multi-channel networks | Multiple declared channels reconcile; sample now covers `settlement` and `audit` network creation | Fablo samples exercise multiple network shapes and generated scripts | Add e2e/status assertions that prove every declared channel is created and joined |
| Multi-org networks | CRD supports multiple orgs; strongest e2e proof is still a one-peer-org sample plus orderer org | Fablo has 2-org CCaaS and private-data samples | Add a BankA/BankB sample with both orgs endorsing on one channel |
| Private data collections | Not modelled | Fablo supports `privateData` collections | Add API design and config package generation for collection config |
| Endorsement policies | `endorsementPolicy` is passed to lifecycle commands; limited validation | Fablo samples include `OR(...)` and `AND(...)` policies | Add multi-org endorsement e2e before broader policy validation |
| Chaincode upgrade | Sequence field exists; install/approve/commit exists | Fablo exposes install/upgrade commands | Add controlled sequence upgrade flow and smoke test |
| CouchDB | Peer DB field exists; CouchDB workloads are not reconciled | Fablo supports CouchDB-backed peers | Add CouchDB Deployment/Service/PVC per peer and rich-query sample |
| Fabric operations UX | Smoke script creates a hand-written tools Job | Fablo CLI gives invoke/query/list commands | Add generated connection profiles, then a `fabricopsctl`/Fablo operation path |
| Job cleanup | Completed Jobs stay for evidence and debugging | Fablo command output is transient | Move readiness evidence to durable status/artifacts, then add TTL cleanup |
| Federated org ownership | Current model assumes one controller can see all org namespaces | Real banks may run separate clusters and own their peers/CAs | Design join bundles and cross-cluster membership workflow |
| Fablo Kubernetes engine | FabricOps has the operator; Fablo has a legacy `engine: kubernetes` sample | Fablo should render/apply FabricOps resources for Kubernetes mode | Build a mapping contract from Fablo config to `FabricNetwork.spec` |
| FabricX readiness | No direct support yet | FabricX is a future ecosystem shift | Track assumptions and avoid hard-coding Fabric 2.x-only concepts |

## Proposed PR Slices

### Slice 1: Parity Roadmap

Purpose: establish the tracked direction for post-demo Fabric and Fablo parity work.

- Add this tracked roadmap.
- Link it from README and `SUPPORTED_FEATURES.md`.
- No runtime code changes.

Exit proof:

- Markdown/docs review.
- `git diff --check`.

### Slice 2: Multi-Org Endorsement Sample

Goal: prove FabricOps can run a two-peer-org channel where chaincode endorsement requires both orgs.

- Add a BankB org to a dedicated sample or e2e fixture.
- Declare a channel with BankA and BankB peers.
- Configure endorsement policy such as `AND('BankAMSP.member','BankBMSP.member')`.
- Ensure lifecycle approvals happen once per org.
- Extend smoke to invoke/query with both orgs.

Exit proof:

- Envtest for desired lifecycle targets.
- Kind e2e showing a valid transaction under a multi-org endorsement policy.

### Slice 3: Private Data Collections Design

Goal: model Fabric private data without rushing the runtime implementation.

- Add an API proposal for `chaincodes[].privateData`.
- Define generated collection config shape and Secret/ConfigMap handling.
- Map Fablo `privateData` entries to the proposed FabricOps API.
- Document upgrade and validation implications.

Exit proof:

- Design doc plus API review checklist.
- No CRD changes until the shape is agreed.

### Slice 4: Private Data Collections Implementation

Goal: package and commit chaincode with collection config.

- Add CRD fields after Slice 3 is accepted.
- Generate collection config artifacts.
- Mount/pass collection config into approve and commit Jobs.
- Add a sample and smoke test that stores/query private data.

Exit proof:

- `make manifests generate`.
- Envtest for artifact/job generation.
- Kind e2e for collection-backed chaincode.

### Slice 5: Network Operations UX Design

Goal: make post-reconcile interaction easy without turning the controller into an imperative CLI.

- Define generated connection profile artifacts.
- Define `fabricopsctl` command boundaries.
- Decide when operations run as in-cluster Jobs versus local Gateway SDK calls.
- Map Fablo CLI commands to FabricOps operation primitives.

Exit proof:

- Design doc and command examples.
- No runtime code changes.

### Slice 6: Finished Job Cleanup Design

Goal: keep namespaces clean without losing readiness truth.

- Inventory generated Jobs and what each proves today.
- Decide durable evidence for channel join, peer join, anchor update, install, approve, and commit.
- Propose `spec.global.jobs` cleanup settings.

Exit proof:

- Design doc plus follow-up issue/PR list.

### Slice 7: Federated Network Joining Design

Goal: support organizations that own their own Kubernetes clusters and join an existing Fabric network.

- Define founder/coordinator and participant responsibilities.
- Define exported join artifacts: MSP definition, TLS roots, peer endpoints, anchor peers, channel references.
- Define imported artifacts: orderer endpoints, TLS roots, channel block/config update material, lifecycle package expectations.
- Decide whether the API should be `FabricNetwork.spec.mode`, a new `FabricParticipant` CRD, or a join-bundle resource.

Exit proof:

- Design doc with a two-cluster kind e2e plan.
- No runtime code changes until API shape is reviewed.

### Slice 8: Fablo Kubernetes Engine Mapping

Goal: make Fablo able to choose Kubernetes as an engine by producing FabricOps resources, while FabricOps remains independently useful.

- Map Fablo `global`, `orgs`, `channels`, `chaincodes`, and `tools` to `FabricNetwork.spec`.
- Define unsupported or Kubernetes-specific fields explicitly.
- Decide whether Fablo installs FabricOps, assumes it exists, or supports both.
- Create a minimal conversion fixture from a Fablo config to the current FabricOps sample shape.

Exit proof:

- Mapping document and fixture.
- No changes in the Fablo repository until the contract is clear.

## Release Checkpoints

- `v0.2.x`: multi-org endorsement proof, updated supported-features matrix, and improved sample coverage.
- `v0.3.x`: one substantial Fabric feature such as private data collections or CouchDB.
- `v0.4.x`: first usable post-reconcile operations UX.
- Later: federated joining and Fablo Kubernetes engine integration after the core API boundaries are stable.
