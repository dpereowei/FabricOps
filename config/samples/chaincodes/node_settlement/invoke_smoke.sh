#!/usr/bin/env bash

set -euo pipefail

NAMESPACE="${NAMESPACE:-fo-sample-banka}"
JOB_NAME="${JOB_NAME:-fabricops-node-settlement-invoke-smoke}"
TIMEOUT="${TIMEOUT:-120s}"
FABRIC_VERSION="${FABRIC_VERSION:-2.5.14}"
CHANNEL="${CHANNEL:-settlement}"
CHAINCODE="${CHAINCODE:-settlement}"
ORG_SLUG="${ORG_SLUG:-banka}"
MSP_ID="${MSP_ID:-BankAMSP}"
ORDERER_ADDRESS="${ORDERER_ADDRESS:-orderer0.fo-sample-orderer.svc.cluster.local:7050}"
ORDERER_TLS_SECRET="${ORDERER_TLS_SECRET:-settlement-orderer0-tls}"
ADMIN_MSP_SECRET="${ADMIN_MSP_SECRET:-banka-admin-msp}"
ADMIN_TLS_SECRET="${ADMIN_TLS_SECRET:-banka-admin-tls}"
SMOKE_ID="${SMOKE_ID:-smoke-$(date +%s)}"
CREATE_FUNCTION="${CREATE_FUNCTION:-createSettlement}"
READ_FUNCTION="${READ_FUNCTION:-readSettlement}"
CHAINCODE_SERVICE_PORT="${CHAINCODE_SERVICE_PORT:-7052}"
DEFAULT_PEER_ADDRESSES="peer0.${NAMESPACE}.svc.cluster.local:7051,peer1.${NAMESPACE}.svc.cluster.local:7051"
DEFAULT_PEER_TLS_SECRETS="peer0-tls,peer1-tls"

if [[ -n "${PEER_ADDRESS:-}" && -z "${PEER_ADDRESSES:-}" ]]; then
  PEER_ADDRESSES="$PEER_ADDRESS"
fi
if [[ -n "${PEER_TLS_SECRET:-}" && -z "${PEER_TLS_SECRETS:-}" ]]; then
  PEER_TLS_SECRETS="$PEER_TLS_SECRET"
fi

PEER_ADDRESSES="${PEER_ADDRESSES:-$DEFAULT_PEER_ADDRESSES}"
PEER_TLS_SECRETS="${PEER_TLS_SECRETS:-$DEFAULT_PEER_TLS_SECRETS}"
PACKAGE_CONFIGMAP="${PACKAGE_CONFIGMAP:-${CHANNEL}-${CHAINCODE}-${ORG_SLUG}-package}"
DEFAULT_EXPECTED_PACKAGE_ADDRESS="${CHANNEL}-${CHAINCODE}-${ORG_SLUG}-{{.peer_hostname}}-ccaas.${NAMESPACE}.svc.cluster.local:${CHAINCODE_SERVICE_PORT}"
EXPECTED_PACKAGE_ADDRESS="${EXPECTED_PACKAGE_ADDRESS:-$DEFAULT_EXPECTED_PACKAGE_ADDRESS}"

IFS=',' read -r -a peer_addresses <<< "$PEER_ADDRESSES"
IFS=',' read -r -a peer_tls_secrets <<< "$PEER_TLS_SECRETS"

if [[ "${#peer_addresses[@]}" -eq 0 ]]; then
  echo "PEER_ADDRESSES must include at least one peer address" >&2
  exit 1
fi
if [[ "${#peer_addresses[@]}" -ne "${#peer_tls_secrets[@]}" ]]; then
  echo "PEER_ADDRESSES and PEER_TLS_SECRETS must contain the same number of comma-separated items" >&2
  exit 1
fi

actual_package_address="$(kubectl -n "$NAMESPACE" get configmap "$PACKAGE_CONFIGMAP" -o jsonpath='{.data.address}')"
if [[ "$actual_package_address" != "$EXPECTED_PACKAGE_ADDRESS" ]]; then
  echo "Expected $PACKAGE_CONFIGMAP address $EXPECTED_PACKAGE_ADDRESS, got $actual_package_address" >&2
  exit 1
fi

peer_tls_roots=()
peer_tls_mounts=""
peer_tls_volumes=""
for i in "${!peer_addresses[@]}"; do
  peer_address="${peer_addresses[$i]}"
  peer_name="${peer_address%%.*}"
  chaincode_service="${CHANNEL}-${CHAINCODE}-${ORG_SLUG}-${peer_name}-ccaas"
  expected_builder="{\"peer_hostname\":\"${peer_name}\"}"
  actual_builder="$(kubectl -n "$NAMESPACE" get deployment "$peer_name" -o jsonpath='{range .spec.template.spec.containers[?(@.name=="peer")].env[?(@.name=="CHAINCODE_AS_A_SERVICE_BUILDER_CONFIG")]}{.value}{end}')"

  if [[ "$actual_builder" != "$expected_builder" ]]; then
    echo "Expected $peer_name builder config $expected_builder, got $actual_builder" >&2
    exit 1
  fi

  kubectl -n "$NAMESPACE" get service "$chaincode_service" >/dev/null
  endpoint_count="$(kubectl -n "$NAMESPACE" get endpointslice -l "kubernetes.io/service-name=${chaincode_service}" -o jsonpath='{range .items[*].endpoints[*]}x{end}')"
  if [[ -z "$endpoint_count" ]]; then
    echo "Expected $chaincode_service to have at least one EndpointSlice endpoint" >&2
    exit 1
  fi
  echo "Verified $peer_name resolves the shared package to $chaincode_service.${NAMESPACE}.svc.cluster.local:${CHAINCODE_SERVICE_PORT}"

  peer_tls_root="/fabricops/smoke/peer-tls/${i}/ca.crt"
  peer_tls_roots+=("$peer_tls_root")
  peer_tls_mounts+="            - name: peer-tls-${i}"$'\n'
  peer_tls_mounts+="              mountPath: /fabricops/smoke/peer-tls/${i}"$'\n'
  peer_tls_mounts+="              readOnly: true"$'\n'
  peer_tls_volumes+="        - name: peer-tls-${i}"$'\n'
  peer_tls_volumes+="          secret:"$'\n'
  peer_tls_volumes+="            secretName: ${peer_tls_secrets[$i]}"$'\n'
  peer_tls_volumes+="            items:"$'\n'
  peer_tls_volumes+="              - key: ca.crt"$'\n'
  peer_tls_volumes+="                path: ca.crt"$'\n'
done

PEER_TLS_ROOTS="$(IFS=','; echo "${peer_tls_roots[*]}")"

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
            - name: FABRICOPS_PEER_ADDRESSES
              value: "${PEER_ADDRESSES}"
            - name: FABRICOPS_PEER_TLS_ROOTS
              value: "${PEER_TLS_ROOTS}"
            - name: FABRICOPS_MSP_ID
              value: "${MSP_ID}"
          command:
            - sh
            - -ec
            - |
              export CORE_PEER_LOCALMSPID="\$FABRICOPS_MSP_ID"
              export CORE_PEER_MSPCONFIGPATH=/fabricops/smoke/admin-msp
              export CORE_PEER_TLS_ENABLED=true
              export CORE_PEER_TLS_CERT_FILE=/fabricops/smoke/admin-tls/client.crt
              export CORE_PEER_TLS_KEY_FILE=/fabricops/smoke/admin-tls/client.key

              old_ifs="\$IFS"
              IFS=","
              set -- \$FABRICOPS_PEER_ADDRESSES
              IFS="\$old_ifs"
              peer_index=1

              for target_peer_address in "\$@"; do
                target_peer_name="\${target_peer_address%%.*}"
                target_peer_tls_root="\$(printf '%s' "\$FABRICOPS_PEER_TLS_ROOTS" | cut -d, -f"\$peer_index")"
                target_smoke_id="\${FABRICOPS_SMOKE_ID}-\${target_peer_name}"
                export CORE_PEER_ADDRESS="\$target_peer_address"
                export CORE_PEER_TLS_ROOTCERT_FILE="\$target_peer_tls_root"

                echo "Invoking \$FABRICOPS_CHAINCODE on \$FABRICOPS_CHANNEL through \$target_peer_name (\$target_peer_address)"

                INVOKE_PAYLOAD="{\"Args\":[\"\$FABRICOPS_CREATE_FUNCTION\",\"\$target_smoke_id\",\"alice\",\"bob\",\"100\",\"USD\"]}"
                QUERY_PAYLOAD="{\"Args\":[\"\$FABRICOPS_READ_FUNCTION\",\"\$target_smoke_id\"]}"

                peer chaincode invoke \
                  -o "\$FABRICOPS_ORDERER_ADDRESS" \
                  --tls \
                  --cafile /fabricops/smoke/orderer-tls/ca.crt \
                  -C "\$FABRICOPS_CHANNEL" \
                  -n "\$FABRICOPS_CHAINCODE" \
                  --peerAddresses "\$target_peer_address" \
                  --tlsRootCertFiles "\$target_peer_tls_root" \
                  -c "\$INVOKE_PAYLOAD" \
                  --waitForEvent

                peer chaincode query \
                  -C "\$FABRICOPS_CHANNEL" \
                  -n "\$FABRICOPS_CHAINCODE" \
                  -c "\$QUERY_PAYLOAD" | tee /tmp/fabricops-query.out

                grep "\$target_smoke_id" /tmp/fabricops-query.out
                peer_index=\$((peer_index + 1))
              done
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
${peer_tls_mounts%$'\n'}
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
${peer_tls_volumes%$'\n'}
YAML

if ! kubectl -n "$NAMESPACE" wait --for=condition=complete "job/$JOB_NAME" --timeout="$TIMEOUT"; then
  kubectl -n "$NAMESPACE" logs "job/$JOB_NAME" || true
  exit 1
fi

kubectl -n "$NAMESPACE" logs "job/$JOB_NAME"
