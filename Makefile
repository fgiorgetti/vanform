VERSION := $(shell git describe --tags --dirty=-modified --always)
REVISION := $(shell git rev-parse HEAD)

PLATFORMS ?= linux/amd64,linux/arm64
REGISTRY := quay.io/fgiorgetti
IMAGE_TAG := main

SKOPEO := skopeo
DOCKER := docker
SHARED_IMAGE_LABELS = \
    --label "org.opencontainers.image.created=$(shell TZ=GMT date --iso-8601=seconds)" \
    --label "org.opencontainers.image.url=https://skupper.io/" \
    --label "org.opencontainers.image.documentation=https://skupper.io/" \
    --label "org.opencontainers.image.source=https://github.com/fgiorgetti/vanform" \
    --label "org.opencontainers.image.version=${VERSION}" \
    --label "org.opencontainers.image.revision=${REVISION}" \
    --label "org.opencontainers.image.licenses=Apache-2.0"

all: vanform

.PHONY: vanform
vanform:
	go build -o vanform ./main.go

oci-archives:
	mkdir -p oci-archives

container-build: oci-archives
	${DOCKER} buildx build \
		"--output=type=oci,dest=$(shell pwd)/oci-archives/vanform.tar" \
		-t "${REGISTRY}/vanform:${IMAGE_TAG}" \
		$(SHARED_IMAGE_LABELS) \
		--platform ${PLATFORMS} \
		-f Containerfile .

container-push: oci-archives
	${SKOPEO} copy --all \
		oci-archive:./oci-archives/vanform.tar \
		"docker://${REGISTRY}/vanform:${IMAGE_TAG}"

format:
	go fmt ./...

vet:
	go vet ./...
