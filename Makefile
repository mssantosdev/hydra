.PHONY: build install clean test run deps fmt vet gate

BINARY_NAME=hydra
INSTALL_PATH=$(HOME)/.local/bin
VERSION ?= dev
COMMIT ?=
BUILT_AT ?=

LDFLAGS=-X github.com/mssantosdev/hydra/internal/cmd.version=$(VERSION)
ifneq ($(strip $(COMMIT)),)
LDFLAGS += -X github.com/mssantosdev/hydra/internal/cmd.commit=$(COMMIT)
endif
ifneq ($(strip $(BUILT_AT)),)
LDFLAGS += -X github.com/mssantosdev/hydra/internal/cmd.builtAt=$(BUILT_AT)
endif

build:
	go build -ldflags "$(LDFLAGS)" -o $(BINARY_NAME) .

install: build
	mkdir -p $(INSTALL_PATH)
	cp $(BINARY_NAME) $(INSTALL_PATH)/
	@echo "Installed to $(INSTALL_PATH)/$(BINARY_NAME)"
	@echo "Make sure $(INSTALL_PATH) is in your PATH"

clean:
	rm -f $(BINARY_NAME)
	go clean

test:
	go test ./...

run: build
	./$(BINARY_NAME)

deps:
	go mod tidy
	go mod download

fmt:
	go fmt ./...

vet:
	go vet ./...

# GOLANGCI_VERSION mirrors the version baked onto the Arvia CI agents by
# arvia-ci-ops. Pinning it locally is the point: a linter version drift means a
# clean local run and a red pipeline.
GOLANGCI_VERSION ?= 2.12.2

# COVERAGE_MIN is the enforced floor for total statement coverage. gate-test fails below it,
# so the number cannot quietly erode one untested branch at a time. Raise it when the real
# figure clears the next step; never lower it to make a change pass.
#
# Measured with -coverpkg=./..., because this is a CLI: the tests in internal/cmd drive
# config, git, hooks and topic for real, and per-package profiling scores that execution as
# zero. It also stops internal/testutil — which every test uses and which has no tests of its
# own — from counting as 141 dead statements.
COVERAGE_MIN ?= 80.0

# gate is the Arvia Go quality gate, run locally in the same order as CI:
# format, vet, lint, vulnerabilities, tests, race.
#
# It additionally runs the race detector, which the CI pipeline currently has
# commented out ("until the CI race-test environment is fixed"). Locally there is
# no such constraint, so hydra does not inherit that gap.
.PHONY: gate gate-fmt gate-vet gate-lint gate-themes gate-docs gate-vuln gate-test gate-race themes
gate: gate-fmt gate-vet gate-lint gate-themes gate-docs gate-vuln gate-test gate-race
	@echo "gate: all checks passed"

themes:
	python3 scripts/gen-themes.py

gate-themes:
	python3 scripts/gen-themes.py --check

gate-docs: build ## Docs claims, checked against the binary they describe
	bash scripts/check-docs-claims.sh

gate-fmt: ## Fail if anything is unformatted
	@out=$$(gofmt -l .); if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi
	@echo "gate: gofmt clean"

gate-vet: ## go vet
	go vet ./...

gate-lint: ## golangci-lint, version-pinned to match CI
	@command -v golangci-lint >/dev/null || { echo "golangci-lint not installed (need $(GOLANGCI_VERSION))"; exit 1; }
	@golangci-lint --version | grep -q '$(GOLANGCI_VERSION)' || { \
		echo "expected golangci-lint $(GOLANGCI_VERSION), got $$(golangci-lint --version)"; exit 1; }
	golangci-lint run ./...

gate-vuln: ## govulncheck against the Go vulnerability database
	@command -v govulncheck >/dev/null || go install golang.org/x/vuln/cmd/govulncheck@latest
	@if command -v govulncheck >/dev/null; then govulncheck ./...; \
	else "$$(go env GOPATH)/bin/govulncheck" ./...; fi

gate-test: ## Tests with coverage, enforced against COVERAGE_MIN
	go test ./... -coverpkg=./... -coverprofile=coverage.out
	@total=$$(go tool cover -func=coverage.out | awk '/^total:/ { gsub(/%/,"",$$NF); print $$NF }'); \
	if [ -z "$$total" ]; then echo "could not read a coverage total from coverage.out"; exit 1; fi; \
	awk -v got="$$total" -v min="$(COVERAGE_MIN)" 'BEGIN { \
		if (got + 0 < min + 0) { \
			printf "coverage %s%% is below the %s%% threshold\n", got, min; exit 1 } \
		printf "coverage %s%% (minimum %s%%)\n", got, min }'

gate-race: ## Race detector (requires cgo)
	CGO_ENABLED=1 go test -race ./...
