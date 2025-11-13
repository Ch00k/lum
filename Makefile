.PHONY: lint test test-verbose test-one test-ci build run release release-patch release-minor release-major

.EXPORT_ALL_VARIABLES:

LUM_EXECUTABLE_FILENAME ?= lum
LUM_BUILD_ARTIFACTS_DIR ?= dist
LUM_VERSION ?= dev

lint:
	golangci-lint run --fix
	prettier --write "assets/"

test:
	gotestsum --format testname -- ./...

test-verbose:
	gotestsum --format standard-verbose -- -v -count=1 ./...

test-one:
	@if [ -z "$(TEST)" ]; then \
		echo "Usage: make test-one TEST=TestName"; \
		exit 1; \
	fi
	gotestsum --format standard-verbose -- -v -count=1 -run "^$(TEST)$$" ./...

test-ci:
	go run gotest.tools/gotestsum@latest --format testname -- -coverprofile=coverage.txt ./...

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
