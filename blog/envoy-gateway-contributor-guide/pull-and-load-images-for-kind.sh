#!/bin/bash
echo "Pull the images and load them into the kind cluster..."
docker pull envoy-gateway-control-plane quay.io/metallb/controller:v0.13.10
docker pull envoy-gateway-control-plane quay.io/metallb/speaker:v0.13.10
docker pull envoy-gateway-control-plane docker.io/jimmysong/gateway-dev:435a4dc1
docker pull envoy-gateway-control-plane envoyproxy/envoy:distroless-dev
docker pull envoy-gateway-control-plane gcr.io/k8s-staging-gateway-api/echo-basic:v20231214-v1.0.0-140-gf544a46e
 
kind load docker-image -n envoy-gateway --nodes envoy-gateway-control-plane quay.io/metallb/controller:v0.13.10
kind load docker-image -n envoy-gateway --nodes envoy-gateway-control-plane quay.io/metallb/speaker:v0.13.10
kind load docker-image -n envoy-gateway --nodes envoy-gateway-control-plane docker.io/jimmysong/gateway-dev:435a4dc1
kind load docker-image -n envoy-gateway --nodes envoy-gateway-control-plane envoyproxy/envoy:distroless-dev
kind load docker-image -n envoy-gateway --nodes envoy-gateway-control-plane gcr.io/k8s-staging-gateway-api/echo-basic:v20231214-v1.0.0-140-gf544a46e