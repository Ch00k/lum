.PHONY: lint test test-verbose test-one test-ci build run update-mermaid release release-patch release-minor release-major

.EXPORT_ALL_VARIABLES:

LUM_BUILD_ARTIFACTS_DIR ?= dist
LUM_EXECUTABLE_FILENAME ?= lum
LUM_TEST_EXECUTABLE_FILENAME ?= lum.test
LUM_TEST_EXECUTABLE ?= $(LUM_BUILD_ARTIFACTS_DIR)/$(LUM_TEST_EXECUTABLE_FILENAME)
LUM_VERSION ?= dev
GOCOVERDIR ?= $(CURDIR)/coverage
MERMAID_VERSION ?= 12.2.0

lint:
	golangci-lint run --fix
	prettier --write "assets/"

test: build-test
	@mkdir -p coverage
	gotestsum --format testname -- ./...

test-verbose: test-binary
	gotestsum --format standard-verbose -- -v -count=1 ./...

test-ci: build-test
	@mkdir -p coverage
	go run gotest.tools/gotestsum@latest --format testname -- -count=1 -coverprofile=coverage-unit.txt ./...
	go tool covdata textfmt -i=$(GOCOVERDIR) -o=coverage-integration.txt

build-test:
	go test -c -cover -o $(LUM_BUILD_ARTIFACTS_DIR)/$(LUM_TEST_EXECUTABLE_FILENAME)

build:
	go build -trimpath -ldflags="-s -w -X main.Version=${LUM_VERSION}" -o ./${LUM_BUILD_ARTIFACTS_DIR}/${LUM_EXECUTABLE_FILENAME} .

run:
	go run . --port 16333 README.md

# Vendor the mermaid runtime and its license from the npm package
update-mermaid:
	curl -sfL -o mermaid.tgz https://registry.npmjs.org/mermaid/-/mermaid-$(MERMAID_VERSION).tgz
	tar -xzOf mermaid.tgz package/dist/mermaid.min.js > assets/mermaid.min.js
	tar -xzOf mermaid.tgz package/LICENSE > assets/mermaid.LICENSE
	rm mermaid.tgz

release:
	@echo "Available release types:"
	@echo "  make release-patch  # Patch version (x.y.Z)"
	@echo "  make release-minor  # Minor version (x.Y.0)"
	@echo "  make release-major  # Major version (X.0.0)"

release-patch:
	./release.sh patch

release-minor:
	./release.sh minor

release-major:
	./release.sh major
