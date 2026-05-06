IMAGE_NAME ?= cert-manager-webhook-vegasystems
IMAGE_TAG  ?= dev
IMAGE      ?= $(IMAGE_NAME):$(IMAGE_TAG)
CHART_DIR  := deploy/cert-manager-webhook-vegasystems

.PHONY: all build test vet image helm-lint helm-package conformance clean

all: vet test build

build:
	CGO_ENABLED=0 go build -trimpath -ldflags '-w -s' -o bin/webhook .

vet:
	go vet ./...

test:
	go test ./...

# Run the cert-manager DNS01 conformance suite against the live VegaSystems API.
# Requires:
#   - TEST_ZONE_NAME (e.g. example.com.)
#   - testdata/vegasystems/secret.yaml with valid api credentials
#   - testdata/vegasystems/config.json with the customer id
#   - kube-apiserver + etcd binaries on PATH (or TEST_ASSET_* env vars set)
conformance:
	go test -tags conformance -v -run TestRunsConformance .

image:
	docker build -t $(IMAGE) .

helm-lint:
	helm lint $(CHART_DIR)

helm-package:
	helm package $(CHART_DIR) -d dist/

clean:
	rm -rf bin/ dist/
