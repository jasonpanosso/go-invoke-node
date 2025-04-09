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
  go build -o /bin/invoke-sam

#################################################
# 2) Runner
#################################################
FROM alpine:latest

RUN apk add --no-cache ca-certificates

COPY --from=builder /bin/invoke-sam /usr/local/bin/invoke-sam

USER nobody:nogroup

ENV PORT=8080 \
  LAMBDA_FUNCTION=MyFunction \
  LAMBDA_ENV_FILE="" \
  TIMEOUT_DURATION=30s \
  TEMPLATE_PATH=template.yaml

EXPOSE ${PORT}

WORKDIR /

ENTRYPOINT ["invoke-sam"]
CMD ["--port=${PORT}", "--function=${LAMBDA_FUNCTION}", "--timeout=${TIMEOUT_DURATION}", "--template=${TEMPLATE_PATH}"]
