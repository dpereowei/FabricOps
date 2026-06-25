#!/usr/bin/env bash

set -euo pipefail

NAMESPACE="${NAMESPACE:-fo-sample-banka}"
JOB_NAME="${JOB_NAME:-fabricops-node-settlement-invoke-smoke}"
TIMEOUT="${TIMEOUT:-120s}"
FABRIC_VERSION="${FABRIC_VERSION:-2.5.14}"
CHANNEL="${CHANNEL:-settlement}"
CHAINCODE="${CHAINCODE:-settlement}"
MSP_ID="${MSP_ID:-BankAMSP}"
ORDERER_ADDRESS="${ORDERER_ADDRESS:-orderer0.fo-sample-orderer.svc.cluster.local:7050}"
ORDERER_TLS_SECRET="${ORDERER_TLS_SECRET:-settlement-orderer0-tls}"
PEER_ADDRESS="${PEER_ADDRESS:-peer0.fo-sample-banka.svc.cluster.local:7051}"
PEER_TLS_SECRET="${PEER_TLS_SECRET:-peer0-tls}"
ADMIN_MSP_SECRET="${ADMIN_MSP_SECRET:-banka-admin-msp}"
ADMIN_TLS_SECRET="${ADMIN_TLS_SECRET:-banka-admin-tls}"
SMOKE_ID="${SMOKE_ID:-smoke-$(date +%s)}"
CREATE_FUNCTION="${CREATE_FUNCTION:-createSettlement}"
READ_FUNCTION="${READ_FUNCTION:-readSettlement}"

kubectl -n "$NAMESPACE" delete job "$JOB_NAME" --ignore-not-found

kubectl -n "$NAMESPACE" apply -f - <<YAML
apiVersion: batch/v1
kind: Job
metadata:
  name: ${JOB_NAME}
  labels:
    app.kubernetes.io/name: fabricops
    app.kubernetes.io/component: chaincode-smoke
spec:
  backoffLimit: 0
  template:
    metadata:
      labels:
        app.kubernetes.io/name: fabricops
        app.kubernetes.io/component: chaincode-smoke
    spec:
      restartPolicy: Never
      containers:
        - name: invoke-smoke
          image: hyperledger/fabric-tools:${FABRIC_VERSION}
          env:
            - name: FABRICOPS_CHANNEL
              value: "${CHANNEL}"
            - name: FABRICOPS_CHAINCODE
              value: "${CHAINCODE}"
            - name: FABRICOPS_SMOKE_ID
              value: "${SMOKE_ID}"
            - name: FABRICOPS_CREATE_FUNCTION
              value: "${CREATE_FUNCTION}"
            - name: FABRICOPS_READ_FUNCTION
              value: "${READ_FUNCTION}"
            - name: FABRICOPS_ORDERER_ADDRESS
              value: "${ORDERER_ADDRESS}"
            - name: FABRICOPS_PEER_ADDRESS
              value: "${PEER_ADDRESS}"
            - name: FABRICOPS_MSP_ID
              value: "${MSP_ID}"
          command:
            - sh
            - -ec
            - |
              export CORE_PEER_LOCALMSPID="\$FABRICOPS_MSP_ID"
              export CORE_PEER_ADDRESS="\$FABRICOPS_PEER_ADDRESS"
              export CORE_PEER_MSPCONFIGPATH=/fabricops/smoke/admin-msp
              export CORE_PEER_TLS_ENABLED=true
              export CORE_PEER_TLS_CERT_FILE=/fabricops/smoke/admin-tls/client.crt
              export CORE_PEER_TLS_KEY_FILE=/fabricops/smoke/admin-tls/client.key
              export CORE_PEER_TLS_ROOTCERT_FILE=/fabricops/smoke/admin-tls/ca.crt

              INVOKE_PAYLOAD="{\"Args\":[\"\$FABRICOPS_CREATE_FUNCTION\",\"\$FABRICOPS_SMOKE_ID\",\"alice\",\"bob\",\"100\",\"USD\"]}"
              QUERY_PAYLOAD="{\"Args\":[\"\$FABRICOPS_READ_FUNCTION\",\"\$FABRICOPS_SMOKE_ID\"]}"

              peer chaincode invoke \
                -o "\$FABRICOPS_ORDERER_ADDRESS" \
                --tls \
                --cafile /fabricops/smoke/orderer-tls/ca.crt \
                -C "\$FABRICOPS_CHANNEL" \
                -n "\$FABRICOPS_CHAINCODE" \
                --peerAddresses "\$FABRICOPS_PEER_ADDRESS" \
                --tlsRootCertFiles /fabricops/smoke/peer-tls/ca.crt \
                -c "\$INVOKE_PAYLOAD" \
                --waitForEvent

              peer chaincode query \
                -C "\$FABRICOPS_CHANNEL" \
                -n "\$FABRICOPS_CHAINCODE" \
                -c "\$QUERY_PAYLOAD" | tee /tmp/fabricops-query.out

              grep "\$FABRICOPS_SMOKE_ID" /tmp/fabricops-query.out
          volumeMounts:
            - name: admin-msp
              mountPath: /fabricops/smoke/admin-msp
              readOnly: true
            - name: admin-tls
              mountPath: /fabricops/smoke/admin-tls
              readOnly: true
            - name: orderer-tls
              mountPath: /fabricops/smoke/orderer-tls
              readOnly: true
            - name: peer-tls
              mountPath: /fabricops/smoke/peer-tls
              readOnly: true
      volumes:
        - name: admin-msp
          secret:
            secretName: ${ADMIN_MSP_SECRET}
            items:
              - key: config.yaml
                path: config.yaml
              - key: cacert.pem
                path: cacerts/ca.pem
              - key: tlscacert.pem
                path: tlscacerts/tlsca.pem
              - key: signcert.pem
                path: signcerts/cert.pem
              - key: keystore.pem
                path: keystore/key.pem
        - name: admin-tls
          secret:
            secretName: ${ADMIN_TLS_SECRET}
            items:
              - key: client.crt
                path: client.crt
              - key: client.key
                path: client.key
              - key: ca.crt
                path: ca.crt
        - name: orderer-tls
          secret:
            secretName: ${ORDERER_TLS_SECRET}
            items:
              - key: ca.crt
                path: ca.crt
        - name: peer-tls
          secret:
            secretName: ${PEER_TLS_SECRET}
            items:
              - key: ca.crt
                path: ca.crt
YAML

if ! kubectl -n "$NAMESPACE" wait --for=condition=complete "job/$JOB_NAME" --timeout="$TIMEOUT"; then
  kubectl -n "$NAMESPACE" logs "job/$JOB_NAME" || true
  exit 1
fi

kubectl -n "$NAMESPACE" logs "job/$JOB_NAME"
