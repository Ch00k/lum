.PHONY: lint test test-verbose test-one test-ci build run release release-patch release-minor release-major

.EXPORT_ALL_VARIABLES:

LUM_BUILD_ARTIFACTS_DIR ?= dist
LUM_EXECUTABLE_FILENAME ?= lum
LUM_TEST_EXECUTABLE_FILENAME ?= lum.test
LUM_TEST_EXECUTABLE ?= $(LUM_BUILD_ARTIFACTS_DIR)/$(LUM_TEST_EXECUTABLE_FILENAME)
LUM_VERSION ?= dev
GOCOVERDIR ?= $(CURDIR)/coverage

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
	go run . test.md

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
