Requirements

golang >= v1.23
kubebuilder >= v4.15.0

fabricops/
├── operator/
│   ├── cmd/
│   ├── controllers/
│   ├── api/
│   ├── main.go
│   └── Dockerfile
├── charts/
│   └── fabric-network/
├── config/
│   ├── crd.yaml
│   └── sample-network.yaml
├── terraform/
│   └── (EMPTY for now)
└── docs/