#!/usr/bin/env bash

set -euo pipefail

NAMESPACE="${NAMESPACE:-fo-sample-banka}"
JOB_NAME="${JOB_NAME:-fabricops-node-settlement-invoke-smoke}"
TIMEOUT="${TIMEOUT:-180s}"
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
RETRY_ATTEMPTS="${RETRY_ATTEMPTS:-6}"
RETRY_DELAY_SECONDS="${RETRY_DELAY_SECONDS:-10}"
CHAINCODE_SERVICE_PORT="${CHAINCODE_SERVICE_PORT:-7052}"
DEFAULT_BANKB_NAMESPACE="${DEFAULT_BANKB_NAMESPACE:-fo-sample-bankb}"
DEFAULT_PEER_ADDRESSES="peer0.${NAMESPACE}.svc.cluster.local:7051,peer1.${NAMESPACE}.svc.cluster.local:7051,peer0.${DEFAULT_BANKB_NAMESPACE}.svc.cluster.local:7051"
DEFAULT_PEER_NAMESPACES="${NAMESPACE},${NAMESPACE},${DEFAULT_BANKB_NAMESPACE}"
DEFAULT_PEER_ORG_SLUGS="${ORG_SLUG},${ORG_SLUG},bankb"
DEFAULT_ENDORSEMENT_SETS="1+3;2+3"

join_csv() {
  local IFS=,
  echo "$*"
}

repeat_csv() {
  local value="$1"
  local count="$2"
  local items=()

  for ((i = 0; i < count; i++)); do
    items+=("$value")
  done

  join_csv "${items[@]}"
}

if [[ -n "${PEER_ADDRESS:-}" && -z "${PEER_ADDRESSES:-}" ]]; then
  PEER_ADDRESSES="$PEER_ADDRESS"
fi
if [[ -n "${PEER_TLS_SECRET:-}" && -z "${PEER_TLS_SECRETS:-}" ]]; then
  PEER_TLS_SECRETS="$PEER_TLS_SECRET"
fi

if [[ -z "${PEER_ADDRESSES:-}" ]]; then
  PEER_ADDRESSES="$DEFAULT_PEER_ADDRESSES"
  PEER_NAMESPACES="${PEER_NAMESPACES:-$DEFAULT_PEER_NAMESPACES}"
  PEER_ORG_SLUGS="${PEER_ORG_SLUGS:-$DEFAULT_PEER_ORG_SLUGS}"
  ENDORSEMENT_SETS="${ENDORSEMENT_SETS:-$DEFAULT_ENDORSEMENT_SETS}"
fi

IFS=',' read -r -a peer_addresses <<< "$PEER_ADDRESSES"

if [[ "${#peer_addresses[@]}" -eq 0 ]]; then
  echo "PEER_ADDRESSES must include at least one peer address" >&2
  exit 1
fi

if [[ -z "${PEER_NAMESPACES:-}" ]]; then
  PEER_NAMESPACES="$(repeat_csv "$NAMESPACE" "${#peer_addresses[@]}")"
fi
if [[ -z "${PEER_TLS_SECRETS:-}" ]]; then
  peer_tls_secret_defaults=()
  for peer_address in "${peer_addresses[@]}"; do
    peer_tls_secret_defaults+=("${peer_address%%.*}-tls")
  done
  PEER_TLS_SECRETS="$(join_csv "${peer_tls_secret_defaults[@]}")"
fi
if [[ -z "${PEER_ORG_SLUGS:-}" ]]; then
  PEER_ORG_SLUGS="$(repeat_csv "$ORG_SLUG" "${#peer_addresses[@]}")"
fi

IFS=',' read -r -a peer_namespaces <<< "$PEER_NAMESPACES"
IFS=',' read -r -a peer_tls_secrets <<< "$PEER_TLS_SECRETS"
IFS=',' read -r -a peer_org_slugs <<< "$PEER_ORG_SLUGS"

if [[ "${#peer_addresses[@]}" -ne "${#peer_namespaces[@]}" ]]; then
  echo "PEER_ADDRESSES and PEER_NAMESPACES must contain the same number of comma-separated items" >&2
  exit 1
fi
if [[ "${#peer_addresses[@]}" -ne "${#peer_tls_secrets[@]}" ]]; then
  echo "PEER_ADDRESSES and PEER_TLS_SECRETS must contain the same number of comma-separated items" >&2
  exit 1
fi
if [[ "${#peer_addresses[@]}" -ne "${#peer_org_slugs[@]}" ]]; then
  echo "PEER_ADDRESSES and PEER_ORG_SLUGS must contain the same number of comma-separated items" >&2
  exit 1
fi

peer_tls_roots=()
peer_tls_mounts=""
peer_tls_volumes=""
peer_labels=()
for i in "${!peer_addresses[@]}"; do
  peer_address="${peer_addresses[$i]}"
  peer_namespace="${peer_namespaces[$i]}"
  peer_tls_secret="${peer_tls_secrets[$i]}"
  peer_org_slug="${peer_org_slugs[$i]}"
  peer_name="${peer_address%%.*}"
  peer_labels+=("${peer_org_slug}-${peer_name}")
  package_configmap="${CHANNEL}-${CHAINCODE}-${peer_org_slug}-package"
  expected_package_address="${CHANNEL}-${CHAINCODE}-${peer_org_slug}-{{.peer_hostname}}-ccaas.${peer_namespace}.svc.cluster.local:${CHAINCODE_SERVICE_PORT}"
  chaincode_service="${CHANNEL}-${CHAINCODE}-${peer_org_slug}-${peer_name}-ccaas"
  expected_builder="{\"peer_hostname\":\"${peer_name}\"}"
  actual_package_address="$(kubectl -n "$peer_namespace" get configmap "$package_configmap" -o jsonpath='{.data.address}')"
  actual_builder="$(kubectl -n "$peer_namespace" get deployment "$peer_name" -o jsonpath='{range .spec.template.spec.containers[?(@.name=="peer")].env[?(@.name=="CHAINCODE_AS_A_SERVICE_BUILDER_CONFIG")]}{.value}{end}')"

  if [[ "$actual_package_address" != "$expected_package_address" ]]; then
    echo "Expected $package_configmap address $expected_package_address, got $actual_package_address" >&2
    exit 1
  fi

  if [[ "$actual_builder" != "$expected_builder" ]]; then
    echo "Expected $peer_name builder config $expected_builder, got $actual_builder" >&2
    exit 1
  fi

  kubectl -n "$peer_namespace" get service "$chaincode_service" >/dev/null
  endpoint_count="$(kubectl -n "$peer_namespace" get endpointslice -l "kubernetes.io/service-name=${chaincode_service}" -o jsonpath='{range .items[*].endpoints[*]}x{end}')"
  if [[ -z "$endpoint_count" ]]; then
    echo "Expected $chaincode_service to have at least one EndpointSlice endpoint" >&2
    exit 1
  fi
  echo "Verified $peer_org_slug/$peer_name resolves the shared package to $chaincode_service.${peer_namespace}.svc.cluster.local:${CHAINCODE_SERVICE_PORT}"

  tls_copy_secret="${JOB_NAME}-peer-${i}-tls"
  encoded_peer_tls_root="$(kubectl -n "$peer_namespace" get secret "$peer_tls_secret" -o jsonpath='{.data.ca\.crt}')"
  if [[ -z "$encoded_peer_tls_root" ]]; then
    echo "Expected $peer_tls_secret in $peer_namespace to contain ca.crt" >&2
    exit 1
  fi
  kubectl -n "$NAMESPACE" apply -f - <<YAML
apiVersion: v1
kind: Secret
metadata:
  name: ${tls_copy_secret}
  labels:
    app.kubernetes.io/name: fabricops
    app.kubernetes.io/component: chaincode-smoke
type: Opaque
data:
  ca.crt: ${encoded_peer_tls_root}
YAML

  peer_tls_root="/fabricops/smoke/peer-tls/${i}/ca.crt"
  peer_tls_roots+=("$peer_tls_root")
  peer_tls_mounts+="            - name: peer-tls-${i}"$'\n'
  peer_tls_mounts+="              mountPath: /fabricops/smoke/peer-tls/${i}"$'\n'
  peer_tls_mounts+="              readOnly: true"$'\n'
  peer_tls_volumes+="        - name: peer-tls-${i}"$'\n'
  peer_tls_volumes+="          secret:"$'\n'
  peer_tls_volumes+="            secretName: ${tls_copy_secret}"$'\n'
  peer_tls_volumes+="            items:"$'\n'
  peer_tls_volumes+="              - key: ca.crt"$'\n'
  peer_tls_volumes+="                path: ca.crt"$'\n'
done

PEER_TLS_ROOTS="$(IFS=','; echo "${peer_tls_roots[*]}")"
PEER_LABELS="$(IFS=','; echo "${peer_labels[*]}")"

if [[ -z "${ENDORSEMENT_SETS:-}" ]]; then
  endorsement_sets=()
  for ((i = 1; i <= ${#peer_addresses[@]}; i++)); do
    endorsement_sets+=("$i")
  done
  ENDORSEMENT_SETS="$(IFS=';'; echo "${endorsement_sets[*]}")"
fi

IFS=';' read -r -a endorsement_sets <<< "$ENDORSEMENT_SETS"
for endorsement_set in "${endorsement_sets[@]}"; do
  if [[ -z "$endorsement_set" ]]; then
    echo "ENDORSEMENT_SETS cannot contain empty entries" >&2
    exit 1
  fi
  IFS='+' read -r -a endorsement_indices <<< "$endorsement_set"
  for endorsement_index in "${endorsement_indices[@]}"; do
    if ! [[ "$endorsement_index" =~ ^[0-9]+$ ]]; then
      echo "ENDORSEMENT_SETS entries must use 1-based numeric peer indexes, got $endorsement_index" >&2
      exit 1
    fi
    if ((endorsement_index < 1 || endorsement_index > ${#peer_addresses[@]})); then
      echo "ENDORSEMENT_SETS index $endorsement_index is outside the PEER_ADDRESSES range" >&2
      exit 1
    fi
  done
done

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
            - name: FABRICOPS_PEER_LABELS
              value: "${PEER_LABELS}"
            - name: FABRICOPS_ENDORSEMENT_SETS
              value: "${ENDORSEMENT_SETS}"
            - name: FABRICOPS_MSP_ID
              value: "${MSP_ID}"
            - name: FABRICOPS_RETRY_ATTEMPTS
              value: "${RETRY_ATTEMPTS}"
            - name: FABRICOPS_RETRY_DELAY_SECONDS
              value: "${RETRY_DELAY_SECONDS}"
          command:
            - sh
            - -ec
            - |
              export CORE_PEER_LOCALMSPID="\$FABRICOPS_MSP_ID"
              export CORE_PEER_MSPCONFIGPATH=/fabricops/smoke/admin-msp
              export CORE_PEER_TLS_ENABLED=true
              export CORE_PEER_TLS_CERT_FILE=/fabricops/smoke/admin-tls/client.crt
              export CORE_PEER_TLS_KEY_FILE=/fabricops/smoke/admin-tls/client.key

              csv_item() {
                printf '%s' "\$1" | cut -d, -f"\$2"
              }

              old_ifs="\$IFS"
              IFS=";"
              set -- \$FABRICOPS_ENDORSEMENT_SETS
              IFS="\$old_ifs"
              set_index=1

              for endorsement_set in "\$@"; do
                first_peer_index="\${endorsement_set%%+*}"
                first_peer_address="\$(csv_item "\$FABRICOPS_PEER_ADDRESSES" "\$first_peer_index")"
                first_peer_tls_root="\$(csv_item "\$FABRICOPS_PEER_TLS_ROOTS" "\$first_peer_index")"
                target_smoke_id="\${FABRICOPS_SMOKE_ID}-set\${set_index}"
                export CORE_PEER_ADDRESS="\$first_peer_address"
                export CORE_PEER_TLS_ROOTCERT_FILE="\$first_peer_tls_root"

                INVOKE_PAYLOAD="{\"Args\":[\"\$FABRICOPS_CREATE_FUNCTION\",\"\$target_smoke_id\",\"alice\",\"bob\",\"100\",\"USD\"]}"
                QUERY_PAYLOAD="{\"Args\":[\"\$FABRICOPS_READ_FUNCTION\",\"\$target_smoke_id\"]}"

                invoke_attempt=1
                while true; do
                  target_labels=""
                  set -- peer chaincode invoke \
                    -o "\$FABRICOPS_ORDERER_ADDRESS" \
                    --tls \
                    --cafile /fabricops/smoke/orderer-tls/ca.crt \
                    -C "\$FABRICOPS_CHANNEL" \
                    -n "\$FABRICOPS_CHAINCODE" \
                    -c "\$INVOKE_PAYLOAD" \
                    --waitForEvent

                  old_ifs="\$IFS"
                  IFS="+"
                  for target_peer_index in \$endorsement_set; do
                    target_peer_address="\$(csv_item "\$FABRICOPS_PEER_ADDRESSES" "\$target_peer_index")"
                    target_peer_tls_root="\$(csv_item "\$FABRICOPS_PEER_TLS_ROOTS" "\$target_peer_index")"
                    target_peer_label="\$(csv_item "\$FABRICOPS_PEER_LABELS" "\$target_peer_index")"
                    target_labels="\${target_labels:+\$target_labels,}\$target_peer_label"
                    set -- "\$@" --peerAddresses "\$target_peer_address" --tlsRootCertFiles "\$target_peer_tls_root"
                  done
                  IFS="\$old_ifs"

                  echo "Invoking \$FABRICOPS_CHAINCODE on \$FABRICOPS_CHANNEL with endorsement set \$set_index (\$target_labels)"

                  if "\$@"; then
                    break
                  fi

                  if [ "\$invoke_attempt" -ge "\$FABRICOPS_RETRY_ATTEMPTS" ]; then
                    echo "Invoke for endorsement set \$set_index (\$target_labels) failed after \$invoke_attempt attempts" >&2
                    exit 1
                  fi

                  echo "Invoke for endorsement set \$set_index was not ready; retrying in \$FABRICOPS_RETRY_DELAY_SECONDS seconds"
                  invoke_attempt=\$((invoke_attempt + 1))
                  sleep "\$FABRICOPS_RETRY_DELAY_SECONDS"
                done

                old_ifs="\$IFS"
                IFS="+"
                for target_peer_index in \$endorsement_set; do
                  target_peer_address="\$(csv_item "\$FABRICOPS_PEER_ADDRESSES" "\$target_peer_index")"
                  target_peer_tls_root="\$(csv_item "\$FABRICOPS_PEER_TLS_ROOTS" "\$target_peer_index")"
                  target_peer_label="\$(csv_item "\$FABRICOPS_PEER_LABELS" "\$target_peer_index")"
                  export CORE_PEER_ADDRESS="\$target_peer_address"
                  export CORE_PEER_TLS_ROOTCERT_FILE="\$target_peer_tls_root"

                  echo "Querying \$FABRICOPS_CHAINCODE on \$FABRICOPS_CHANNEL through \$target_peer_label (\$target_peer_address)"

                  query_attempt=1
                  while true; do
                    if peer chaincode query \
                      -C "\$FABRICOPS_CHANNEL" \
                      -n "\$FABRICOPS_CHAINCODE" \
                      -c "\$QUERY_PAYLOAD" | tee /tmp/fabricops-query.out &&
                      grep "\$target_smoke_id" /tmp/fabricops-query.out; then
                      break
                    fi

                    if [ "\$query_attempt" -ge "\$FABRICOPS_RETRY_ATTEMPTS" ]; then
                      echo "Query through \$target_peer_label failed after \$query_attempt attempts" >&2
                      exit 1
                    fi

                    echo "Query through \$target_peer_label was not ready; retrying in \$FABRICOPS_RETRY_DELAY_SECONDS seconds"
                    query_attempt=\$((query_attempt + 1))
                    sleep "\$FABRICOPS_RETRY_DELAY_SECONDS"
                  done
                done
                IFS="\$old_ifs"
                set_index=\$((set_index + 1))
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
