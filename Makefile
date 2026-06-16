SHELL         := /bin/bash
.SHELLFLAGS   := -eu -o pipefail -c
.DEFAULT_GOAL := help

# Project
APP             := frridge
MP_APP          := frridge-mp
APPS            := $(APP) $(MP_APP)
PACKAGE_VERSION ?= $(if $(TAG),$(TAG),$(shell git describe --tags --dirty --always 2>/dev/null || printf 'dev'))
VERSION_PKG     := frridge/internal/buildinfo

# Output directories
BINDIR  := bin
DISTDIR := dist

# Toolchain
GO         ?= go
STATICCHECK ?= $(GO) run honnef.co/go/tools/cmd/staticcheck@latest
REVIVE      ?= $(GO) run github.com/mgechev/revive@latest -formatter friendly
MODERNIZE   ?= $(GO) run golang.org/x/tools/gopls/internal/analysis/modernize/cmd/modernize@latest -test
GO_LDFLAGS  := -s -w -X $(VERSION_PKG).Version=$(PACKAGE_VERSION)
HOST_GOOS   := $(shell $(GO) env GOOS)
HOST_GOARCH := $(shell $(GO) env GOARCH)
GO_PACKAGES := ./...
GOFILES     := $(shell git ls-files --cached --others --exclude-standard -- '*.go' | while read -r file; do [ -f "$$file" ] && printf '%s\n' "$$file"; done)

# Commands
INSTALL ?= install
GH      ?= gh
SHASUM  ?= shasum

# Install
INSTALL_PREFIX ?= $(HOME)/.local
INSTALL_BINDIR ?= $(INSTALL_PREFIX)/bin

# Multipass
MP_NAME         ?= frridge-dev
MP_CPUS         ?= 2
MP_MEM          ?= 4G
MP_DISK         ?= 20G
MP_IMAGE        ?= 24.04
MP_KEEP         ?= 0
VERIFY_LAB      ?= testdata/smoke/lab.yaml
VERIFY_HOST_DIR := $(abspath $(dir $(VERIFY_LAB)))
VERIFY_PKG      := ./internal/multipass
VERIFY_TEST     := TestMultipassSmoke

# Release
GIT_REMOTE   ?= origin
RELEASE_MAKE ?= $(MAKE)
OS           ?= darwin,linux
ARCH         ?= amd64,arm64
DIST_TAG     ?= $(PACKAGE_VERSION)
DIST_APP     := $(APP)-$(DIST_TAG)

# Help
HELP_NAME_WIDTH    := 17
HELP_EXAMPLE_WIDTH := 44

##@ Development

.PHONY: build
build: ## Build host binaries into bin/
	@mkdir -p "$(BINDIR)"
	@for app in $(APPS); do \
		CGO_ENABLED=0 "$(GO)" build -trimpath -ldflags "$(GO_LDFLAGS)" -o "$(BINDIR)/$$app" "./cmd/$$app"; \
		chmod +x "$(BINDIR)/$$app"; \
		printf 'Wrote %s/%s\n' "$(BINDIR)" "$$app"; \
	done

.PHONY: install
install: ## Build and install host binaries into INSTALL_BINDIR
	@$(MAKE) --no-print-directory build
	@mkdir -p "$(INSTALL_BINDIR)"
	@for app in $(APPS); do \
		"$(INSTALL)" -m 0755 "$(BINDIR)/$$app" "$(INSTALL_BINDIR)/$$app"; \
		printf 'Installed %s\n' "$(INSTALL_BINDIR)/$$app"; \
	done

.PHONY: fmt
fmt: ## Format Go sources. Use CHECK_ONLY=1 to check without writing
	@if [ "$(CHECK_ONLY)" = "1" ]; then \
		out="$$(gofmt -l $(GOFILES))"; \
		if [ -n "$$out" ]; then \
			printf '%s\n' "$$out"; \
			exit 1; \
		fi; \
	else \
		gofmt -w $(GOFILES); \
	fi

.PHONY: lint
lint: ## Run Go static analysis
	@"$(GO)" vet $(GO_PACKAGES)
	@$(STATICCHECK) $(GO_PACKAGES)
	@$(REVIVE) $(GO_PACKAGES)
	@$(MODERNIZE) $(GO_PACKAGES)

.PHONY: test
test: ## Run unit tests
	@"$(GO)" test $(GO_PACKAGES)

.PHONY: check
check: ## Run formatting, lint, and unit tests
	@$(MAKE) --no-print-directory fmt CHECK_ONLY=1
	@$(MAKE) --no-print-directory lint
	@$(MAKE) --no-print-directory test

.PHONY: clean
clean: ## Remove local build artifacts
	@rm -rf "$(BINDIR)" "$(DISTDIR)"

##@ Multipass

.PHONY: mp-shell
mp-shell: ## Open a shell in the Multipass workspace
	@"$(GO)" run ./cmd/frridge-mp \
		--instance "$(MP_NAME)" \
		--image "$(MP_IMAGE)" \
		--cpus "$(MP_CPUS)" \
		--memory "$(MP_MEM)" \
		--disk "$(MP_DISK)" \
		--repo-dir "$(CURDIR)" \
		--host-dir "$(VERIFY_HOST_DIR)" \
		shell

.PHONY: mp-verify
mp-verify: ## Run the Multipass-backed smoke test
	@FRRIDGE_RUN_INTEGRATION=1 \
	FRRIDGE_KEEP_SMOKE="$(MP_KEEP)" \
	FRRIDGE_VERIFY_LAB="$(abspath $(VERIFY_LAB))" \
	MP_NAME="$(MP_NAME)" \
	MP_IMAGE="$(MP_IMAGE)" \
	MP_CPUS="$(MP_CPUS)" \
	MP_MEM="$(MP_MEM)" \
	MP_DISK="$(MP_DISK)" \
	"$(GO)" test -tags=integration -count=1 -run "$(VERIFY_TEST)" -timeout 30m -v "$(VERIFY_PKG)"

.PHONY: mp-stop
mp-stop: ## Stop the Multipass VM
	@multipass stop "$(MP_NAME)"

.PHONY: mp-delete
mp-delete: ## Delete the Multipass VM and purge local Multipass state
	-@multipass delete "$(MP_NAME)"
	-@multipass purge

.PHONY: mp-status
mp-status: ## Show Multipass VM status
	@multipass info "$(MP_NAME)"

define RELEASE_SCRIPT
# shellcheck shell=bash
set -Eeuo pipefail

fail() {
  echo "release: $$*" >&2
  exit 1
}

run() {
  printf '+'
  printf ' %q' "$$@"
  printf '\n'
  "$$@"
}

need() {
  command -v "$$1" >/dev/null 2>&1 || fail "$$1 is required for release"
}

clean_git_dir() {
  local dir="$$1" label="$$2" status

  git -C "$$dir" rev-parse --is-inside-work-tree >/dev/null 2>&1 || fail "$$label repo not found at $$dir"
  status="$$(git -C "$$dir" status --porcelain)"
  if [[ -n "$$status" ]]; then
    git -C "$$dir" status --short >&2
    fail "$$label must be clean before release"
  fi
}

github_repo() {
  local repo="$${GH_REPO:-$${GITHUB_REPOSITORY:-}}" url

  if [[ -z "$$repo" ]]; then
    url="$$(git config --get "remote.$$GIT_REMOTE.url" || true)"
    case "$$url" in
      git@github.com:*) repo="$${url#git@github.com:}" ;;
      https://github.com/*) repo="$${url#https://github.com/}" ;;
      ssh://git@github.com/*) repo="$${url#ssh://git@github.com/}" ;;
      *) fail "could not infer GitHub repository from remote $$GIT_REMOTE; set GH_REPO=owner/repo" ;;
    esac
  fi

  repo="$${repo#https://github.com/}"
  repo="$${repo%.git}"
  [[ "$$repo" == */* ]] || fail "GitHub repository must look like owner/repo, got $$repo"
  printf '%s\n' "$$repo"
}

cleanup() {
  local status=$$?

  if [[ "$$created_tag" == 1 && "$$pushed_tag" != 1 ]]; then
    git tag -d "$$TAG" >/dev/null 2>&1 || true
  fi

  exit "$$status"
}

semver='^v[0-9]+[.][0-9]+[.][0-9]+(-[0-9A-Za-z][0-9A-Za-z.-]*)?([+][0-9A-Za-z][0-9A-Za-z.-]*)?$$'
created_tag=0
pushed_tag=0

[[ -n "$$TAG" ]] || fail "TAG is required, for example: make release TAG=v0.1.0"
[[ "$$TAG" =~ $$semver ]] || fail "TAG must look like vMAJOR.MINOR.PATCH"

cd "$$(git rev-parse --show-toplevel)"
clean_git_dir . "working tree"
need git
need "$$GH"
need "$$SHASUM"

repo="$$(github_repo)"
remote_line="$$(git ls-remote --tags "$$GIT_REMOTE" "refs/tags/$$TAG" | sed -n '1p')"
remote_oid="$${remote_line%%[[:space:]]*}"
trap cleanup EXIT

if git rev-parse -q --verify "refs/tags/$$TAG" >/dev/null; then
  local_oid="$$(git rev-parse "refs/tags/$$TAG")"
  [[ -z "$$remote_oid" || "$$remote_oid" == "$$local_oid" ]] || \
    fail "local tag $$TAG does not match $$GIT_REMOTE/tags/$$TAG"
  printf 'Using existing tag %s at %s\n' "$$TAG" "$$(git rev-list -n 1 "$$TAG")"
elif [[ -n "$$remote_oid" ]]; then
  run git fetch "$$GIT_REMOTE" "refs/tags/$$TAG:refs/tags/$$TAG"
  printf 'Using fetched tag %s at %s\n' "$$TAG" "$$(git rev-list -n 1 "$$TAG")"
else
  run git tag "$$TAG"
  created_tag=1
  printf 'Created tag %s at %s\n' "$$TAG" "$$(git rev-parse HEAD)"
fi

release_commit="$$(git rev-list -n 1 "$$TAG")"
head_commit="$$(git rev-parse HEAD)"
[[ "$$release_commit" == "$$head_commit" ]] || \
  fail "$$TAG points to $$release_commit, but HEAD is $$head_commit; checkout the release commit first"

run "$$RELEASE_MAKE" dist TAG="$$TAG" OS="$$OS" ARCH="$$ARCH"
run git push "$$GIT_REMOTE" "refs/tags/$$TAG"
pushed_tag=1

shopt -s nullglob
assets=("$$DISTDIR"/*)
shopt -u nullglob
(($${#assets[@]} > 0)) || fail "no release assets found in $$DISTDIR"

release_flags=()
[[ "$$TAG" == *-* ]] && release_flags=(--prerelease)

if "$$GH" release view "$$TAG" --repo "$$repo" >/dev/null 2>&1; then
  run "$$GH" release upload "$$TAG" "$${assets[@]}" --clobber --repo "$$repo"
else
  run "$$GH" release create "$$TAG" \
    --repo "$$repo" \
    --target "$$release_commit" \
    --title "$$TAG" \
    --generate-notes \
    "$${release_flags[@]}" \
    "$${assets[@]}"
fi

printf 'Published %s using local dist artifacts.\n' "$$TAG"
endef
export RELEASE_SCRIPT

##@ Distribution

.PHONY: release
release: ## Build dist artifacts and publish them to a GitHub release. Requires TAG=vX.Y.Z
	@GH="$(GH)" SHASUM="$(SHASUM)" TAG="$(TAG)" GIT_REMOTE="$(GIT_REMOTE)" DISTDIR="$(DISTDIR)" OS="$(OS)" ARCH="$(ARCH)" RELEASE_MAKE="$(RELEASE_MAKE)" bash -c "$$RELEASE_SCRIPT"

.PHONY: dist
dist: ## Build release tarballs into dist/. Use OS=darwin,linux and ARCH=amd64,arm64
	@rm -rf "$(DISTDIR)"
	@mkdir -p "$(DISTDIR)"
	@os_list="$(OS)"; \
	arch_list="$(ARCH)"; \
	if [ -z "$$os_list" ]; then \
		echo "OS is required. Supported values: darwin,linux" >&2; \
		exit 1; \
	fi; \
	if [ -z "$$arch_list" ]; then \
		echo "ARCH is required. Supported values: amd64,arm64" >&2; \
		exit 1; \
	fi; \
	for os in $$(printf '%s' "$$os_list" | tr ',' ' '); do \
		case "$$os" in \
			darwin|linux) ;; \
			*) echo "Unsupported OS '$$os'. Supported values: darwin,linux" >&2; exit 1 ;; \
		esac; \
	done; \
	for arch in $$(printf '%s' "$$arch_list" | tr ',' ' '); do \
		case "$$arch" in \
			amd64|arm64) ;; \
			*) echo "Unsupported ARCH '$$arch'. Supported values: amd64,arm64" >&2; exit 1 ;; \
		esac; \
	done; \
	for os in $$(printf '%s' "$$os_list" | tr ',' ' '); do \
		for arch in $$(printf '%s' "$$arch_list" | tr ',' ' '); do \
			$(MAKE) --no-print-directory _dist.$$os.$$arch TAG="$(TAG)" || exit $$?; \
		done; \
	done; \
	$(MAKE) --no-print-directory dist-smoke TAG="$(TAG)" || exit $$?; \
	$(MAKE) --no-print-directory checksums TAG="$(TAG)" || exit $$?

.PHONY: dist-smoke
dist-smoke: ## Smoke-test the host-matching dist tarball
	@archive="$(DISTDIR)/$(DIST_APP)-$(HOST_GOOS)-$(HOST_GOARCH).tar.gz"; \
	if [ ! -f "$$archive" ]; then \
		printf 'Skipping host dist smoke test; no host artifact found at %s\n' "$$archive"; \
		exit 0; \
	fi; \
	tmpdir="$$(mktemp -d "$(DISTDIR)/smoke.XXXXXX")"; \
	trap 'rm -rf "$$tmpdir"' EXIT; \
	tar -C "$$tmpdir" -xzf "$$archive"; \
	bundle_dir="$$tmpdir/$(DIST_APP)-$(HOST_GOOS)-$(HOST_GOARCH)"; \
	"$$bundle_dir/$(APP)" --help >/dev/null; \
	"$$bundle_dir/$(APP)" --version >/dev/null; \
	"$$bundle_dir/$(MP_APP)" --help >/dev/null; \
	"$$bundle_dir/$(MP_APP)" --version >/dev/null; \
	printf 'Smoke-tested %s\n' "$$archive"

.PHONY: checksums
checksums: ## Write SHA-256 checksums for dist artifacts
	@if [ ! -d "$(DISTDIR)" ] || ! ls "$(DISTDIR)"/$(DIST_APP)-*.tar.gz >/dev/null 2>&1; then \
		echo "No dist artifacts found" >&2; \
		exit 1; \
	fi
	@cd "$(DISTDIR)" && "$(SHASUM)" -a 256 $(DIST_APP)-*.tar.gz > checksums.txt
	@printf 'Wrote %s/checksums.txt\n' "$(DISTDIR)"

define DIST_RULE
.PHONY: _dist.$(1).$(2)
_dist.$(1).$(2):
	@bundle="$(DIST_APP)-$(1)-$(2)"; \
	stage="$(DISTDIR)/$$$$bundle"; \
	rm -rf "$$$$stage" "$$$$stage.tar.gz"; \
	mkdir -p "$$$$stage"; \
	CGO_ENABLED=0 GOOS="$(1)" GOARCH="$(2)" "$(GO)" build -trimpath -ldflags "$(GO_LDFLAGS)" -o "$$$$stage/$(APP)" ./cmd/$(APP); \
	CGO_ENABLED=0 GOOS="$(1)" GOARCH="$(2)" "$(GO)" build -trimpath -ldflags "$(GO_LDFLAGS)" -o "$$$$stage/$(MP_APP)" ./cmd/$(MP_APP); \
	chmod +x "$$$$stage/$(APP)" "$$$$stage/$(MP_APP)"; \
	cp README.md "$$$$stage/README.md"; \
	tar -C "$(DISTDIR)" -czf "$(DISTDIR)/$$$$bundle.tar.gz" "$$$$bundle"; \
	rm -rf "$$$$stage"; \
	printf 'Wrote %s/%s.tar.gz\n' "$(DISTDIR)" "$$$$bundle"
endef
$(foreach os,darwin linux,$(foreach arch,amd64 arm64,$(eval $(call DIST_RULE,$(os),$(arch)))))

##@ Help

.PHONY: help
help: ## Show this help message
	@awk -v width="$(HELP_NAME_WIDTH)" 'BEGIN {FS = ":.*##"} \
		{ lines[NR] = $$0 } \
		END { \
			section = ""; \
			for (i = 1; i <= NR; i++) { \
				$$0 = lines[i]; \
				if ($$0 ~ /^##@/) { \
					section = substr($$0, 5); \
				} else if ($$0 ~ /^[a-zA-Z0-9_.-]+:.*##/) { \
					split($$0, parts, ":.*##"); \
					sub(/^[[:space:]]+/, "", parts[2]); \
					if (section != "") printf "\n\033[1m%s\033[0m\n", section; \
					section = ""; \
					printf "  \033[36m%-*s\033[0m%s\n", width, parts[1], parts[2]; \
				} \
			} \
		}' $(MAKEFILE_LIST)
	@printf "\n\033[1mVariables:\033[0m\n"
	@printf "  \033[36m%-*s\033[0m%s\n" "$(HELP_NAME_WIDTH)" "TAG" "Release tag for make release, for example v0.1.0"
	@printf "  \033[36m%-*s\033[0m%s\n" "$(HELP_NAME_WIDTH)" "PACKAGE_VERSION" "Build version, defaults to git describe or TAG"
	@printf "  \033[36m%-*s\033[0m%s\n" "$(HELP_NAME_WIDTH)" "GIT_REMOTE" "Release git remote, defaults to $(GIT_REMOTE)"
	@printf "  \033[36m%-*s\033[0m%s\n" "$(HELP_NAME_WIDTH)" "OS" "Release OS list for make dist, defaults to $(OS)"
	@printf "  \033[36m%-*s\033[0m%s\n" "$(HELP_NAME_WIDTH)" "ARCH" "Release arch list for make dist, defaults to $(ARCH)"
	@printf "  \033[36m%-*s\033[0m%s\n" "$(HELP_NAME_WIDTH)" "INSTALL_BINDIR" "Install directory, defaults to $(INSTALL_BINDIR)"
	@printf "  \033[36m%-*s\033[0m%s\n" "$(HELP_NAME_WIDTH)" "MP_NAME" "Multipass instance name, defaults to $(MP_NAME)"
	@printf "  \033[36m%-*s\033[0m%s\n" "$(HELP_NAME_WIDTH)" "VERIFY_LAB" "Topology used by make mp-verify, defaults to $(VERIFY_LAB)"
	@printf "\n\033[1mExamples:\033[0m\n"
	@printf "  \033[36m%-*s\033[0m%s\n" "$(HELP_EXAMPLE_WIDTH)" "make build" "# Build host binaries with git-describe version metadata"
	@printf "  \033[36m%-*s\033[0m%s\n" "$(HELP_EXAMPLE_WIDTH)" "make check" "# Run formatting, lint, and unit tests"
	@printf "  \033[36m%-*s\033[0m%s\n" "$(HELP_EXAMPLE_WIDTH)" "make mp-verify" "# Run the Multipass-backed smoke test"
	@printf "  \033[36m%-*s\033[0m%s\n" "$(HELP_EXAMPLE_WIDTH)" "make dist OS=darwin,linux ARCH=amd64,arm64" "# Build release tarballs and checksums"
	@printf "  \033[36m%-*s\033[0m%s\n" "$(HELP_EXAMPLE_WIDTH)" "make release TAG=v0.1.0" "# Publish a GitHub release from local dist artifacts"
