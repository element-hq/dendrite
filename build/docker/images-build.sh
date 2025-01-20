#!/usr/bin/env bash

cd $(git rev-parse --show-toplevel)

TAG=${1:-latest}

echo "Building tag '${TAG}'"

docker build -t ghcr.io/element-hq/dendrite-monolith:binaries --target build .

docker build -t ghcr.io/element-hq/dendrite-monolith:${TAG} .

docker build -t ghcr.io/element-hq/dendrite-demo-yggdrasil:${TAG} -f build/docker/Dockerfile.demo-yggdrasil .
docker build -t ghcr.io/element-hq/dendrite-demo-pinecone:${TAG} -f build/docker/Dockerfile.demo-pinecone .