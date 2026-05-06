FROM golang:1.25-alpine AS build

RUN apk add --no-cache git
WORKDIR /workspace

COPY go.mod go.sum ./
RUN go mod download

COPY . .
ARG TARGETOS=linux
ARG TARGETARCH=amd64
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags '-w -s -extldflags "-static"' -o /out/webhook .

FROM alpine:3.20
RUN apk add --no-cache ca-certificates && \
    addgroup -S -g 65532 webhook && \
    adduser  -S -u 65532 -G webhook webhook
COPY --from=build /out/webhook /usr/local/bin/webhook
USER 65532:65532
ENTRYPOINT ["/usr/local/bin/webhook"]
