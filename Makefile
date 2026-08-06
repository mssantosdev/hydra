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

# gate is the Arvia Go quality gate, run locally in the same order as CI:
# format, vet, lint, vulnerabilities, tests, race.
#
# It additionally runs the race detector, which the CI pipeline currently has
# commented out ("until the CI race-test environment is fixed"). Locally there is
# no such constraint, so hydra does not inherit that gap.
.PHONY: gate gate-fmt gate-vet gate-lint gate-themes gate-guide gate-vuln gate-test gate-race themes pages
gate: gate-fmt gate-vet gate-lint gate-themes gate-guide gate-vuln gate-test gate-race
	@echo "gate: all checks passed"

pages:
	@# Publish docs/guide.html as the entire site on an orphan gh-pages branch.
	@# Pages sourced from /docs would serve every .md in that tree as its own URL — harmless
	@# on a public repo, but it publishes pages nobody reviewed and couples a docs edit to a
	@# site deploy. This keeps the published surface to exactly the one file.
	@#
	@# Publishes from HEAD, never the working tree: `make pages` mid-edit would publish a
	@# half-finished page, and the published artifact should correspond to a commit.
	@#
	@# Appends to gh-pages rather than force-pushing. A force-push produced overlapping
	@# uploads in one deploy-pages run ("Multiple artifacts named github-pages", count 3)
	@# and the deployment failed while the Pages API kept reporting "building".
	@set -e; \
	tmp=$$(mktemp -d); \
	git show HEAD:docs/guide.html > $$tmp/index.html; \
	touch $$tmp/.nojekyll; \
	email=$$(git config user.email); name=$$(git config user.name); \
	origin=$$(git remote get-url origin); rev=$$(git rev-parse --short HEAD); \
	cd $$tmp && git init -q -b gh-pages . && \
	git fetch -q "$$origin" gh-pages && git reset -q --mixed FETCH_HEAD; \
	git add -A && \
	git -c user.email="$$email" -c user.name="$$name" \
	    commit -q -m "publish guide from $$rev" && \
	git push -q "$$origin" gh-pages:gh-pages; \
	rm -rf $$tmp
themes:
	python3 scripts/gen-themes.py

gate-themes:
	python3 scripts/gen-themes.py --check

gate-guide:
	bash scripts/check-guide-version.sh

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

gate-test: ## Tests with coverage
	go test ./... -coverprofile=coverage.out
	@go tool cover -func=coverage.out | tail -1

gate-race: ## Race detector (requires cgo)
	CGO_ENABLED=1 go test -race ./...
