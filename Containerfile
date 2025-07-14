FROM --platform=$BUILDPLATFORM golang:1.24 AS builder

ARG TARGETARCH

RUN apt update && apt install -y jq

WORKDIR /go/src/app
COPY go.mod .
COPY go.sum .
RUN go mod download
COPY . .

ENV CGO_ENABLED=0
RUN make vanform GOARCH=$TARGETARCH

# Use scratch for minimal image size
FROM scratch

LABEL \
  org.opencontainers.image.title="Skupper Van Form Controller" \
  org.opencontainers.image.description="Publishes and Consumes Skupper token from Vault, linking your VAN."

WORKDIR /app

# Copy the statically linked binary
COPY --from=builder /go/src/app/vanform .

# Use numeric user ID (no need to create user in scratch)
USER 10000

ENTRYPOINT ["/app/vanform"]
