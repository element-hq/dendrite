#!/usr/bin/env bash

TAG=${1:-latest}

echo "Pushing tag '${TAG}'"

docker push ghcr.io/element-hq/dendrite-monolith:${TAG}
