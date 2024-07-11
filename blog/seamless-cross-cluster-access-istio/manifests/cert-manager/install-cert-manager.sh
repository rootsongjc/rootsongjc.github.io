#/bin/bash

set -e

export CTX_CLUSTER1=cluster-1
export CTX_CLUSTER2=cluster-2

kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.15.1/cert-manager.yaml --context="${CTX_CLUSTER1}"
kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.15.1/cert-manager.yaml --context="${CTX_CLUSTER2}"
kubectl apply -f selfsigned-issuer.yaml -f istiod-cert.yaml --context="${CTX_CLUSTER1}"
kubectl apply -f selfsigned-issuer.yaml -f istiod-cert.yaml --context="${CTX_CLUSTER2}"