# syntax=docker/dockerfile:1

#################################################
# 1) Builder
#################################################
FROM golang:1.24-alpine AS builder

ARG TARGETARCH
ARG TARGETOS

USER root
WORKDIR /app

RUN apk add --no-cache git ca-certificates

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 \
  GOOS=${TARGETOS} \
  GOARCH=${TARGETARCH} \
  go build -o /bin/invoke-node

#################################################
# 2) Runner
#################################################
FROM node:20-alpine

RUN apk add ca-certificates

COPY --from=builder /bin/invoke-node /usr/local/bin/invoke-node

USER nobody:nogroup

ENV PORT=8080 \
  SCRIPT="" \
  SCRIPT_FILE="" \
  ENV_FILE="" \
  TIMEOUT_DURATION="30s"

EXPOSE ${PORT}

WORKDIR /

ENTRYPOINT ["invoke-node"]
