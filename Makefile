NAME=namecheap
BINARY=terraform-provider-${NAME}
VERSION=2.0.0
OS_ARCH=darwin_amd64

format:
	go fmt ./...

check:
	go vet ./...

test:
	go test -v ./namecheap/... -count=1 -covermode=atomic -coverprofile=coverage.out

# Live-API acceptance tests against the real Namecheap sandbox.
# Please set the following ENV variables for this test:
# NAMECHEAP_USER_NAME, NAMECHEAP_API_USER, NAMECHEAP_API_KEY, NAMECHEAP_TEST_DOMAIN, NAMECHEAP_USE_SANDBOX (optional, default is false)
# `testacc` is kept as a backwards-compatible alias for `testacc-sandbox`.
testacc: testacc-sandbox

testacc-sandbox:
	TF_ACC=1 go test ./namecheap -v -run=TestAcc -count=1 -timeout=30m -failfast

# Mock-backed acceptance tests: run the acceptance suite against an in-process
# stateful mock of the Namecheap API (no credentials, no network). Requires the
# `testacc` build tag so the NAMECHEAP_API_URL endpoint override is compiled in,
# and a CLI binary. Honors a pre-set TF_ACC_TERRAFORM_PATH (so CI can point it at
# terraform or tofu); otherwise falls back to the terraform on PATH.
#
# It writes its own coverage profile, separate from `test`'s. The provider runs
# in-process under the SDKv2 test harness, so this suite is what exercises the
# CRUD paths — a resource's Create/Read/Update/Delete are only reachable through
# real Terraform. Uploading only `test`'s profile reports those paths as untested
# when they are the most thoroughly tested code in the repo, so CI sends both (see
# the Codecov step in ci.yml).
testacc-mock:
	TF_ACC=1 TF_ACC_TERRAFORM_PATH="$${TF_ACC_TERRAFORM_PATH:-$$(command -v terraform)}" go test -tags testacc ./namecheap -v -run='^TestAccMock' -count=1 -timeout=10m -covermode=atomic -coverprofile=coverage-acceptance.out

build:
	go build -o ${BINARY}

release:
	GOOS=darwin GOARCH=amd64 go build -o ./bin/${BINARY}_${VERSION}_darwin_amd64
	GOOS=darwin GOARCH=arm64 go build -o ./bin/${BINARY}_${VERSION}_darwin_arm64
	GOOS=freebsd GOARCH=386 go build -o ./bin/${BINARY}_${VERSION}_freebsd_386
	GOOS=freebsd GOARCH=amd64 go build -o ./bin/${BINARY}_${VERSION}_freebsd_amd64
	GOOS=freebsd GOARCH=arm go build -o ./bin/${BINARY}_${VERSION}_freebsd_arm
	GOOS=linux GOARCH=386 go build -o ./bin/${BINARY}_${VERSION}_linux_386
	GOOS=linux GOARCH=amd64 go build -o ./bin/${BINARY}_${VERSION}_linux_amd64
	GOOS=linux GOARCH=arm go build -o ./bin/${BINARY}_${VERSION}_linux_arm
	GOOS=openbsd GOARCH=386 go build -o ./bin/${BINARY}_${VERSION}_openbsd_386
	GOOS=openbsd GOARCH=amd64 go build -o ./bin/${BINARY}_${VERSION}_openbsd_amd64
	GOOS=solaris GOARCH=amd64 go build -o ./bin/${BINARY}_${VERSION}_solaris_amd64
	GOOS=windows GOARCH=386 go build -o ./bin/${BINARY}_${VERSION}_windows_386
	GOOS=windows GOARCH=amd64 go build -o ./bin/${BINARY}_${VERSION}_windows_amd64

install_darwin_amd64: build
	mkdir -p ~/.terraform.d/plugins/localhost/namecheap/${NAME}/${VERSION}/darwin_amd64
	mv ${BINARY} ~/.terraform.d/plugins/localhost/namecheap/${NAME}/${VERSION}/darwin_amd64

install_linux_amd64: build
	mkdir -p ~/.terraform.d/plugins/localhost/namecheap/${NAME}/${VERSION}/linux_amd64
	mv ${BINARY} ~/.terraform.d/plugins/localhost/namecheap/${NAME}/${VERSION}/linux_amd64

# Make sure you have installed golangci-lint CLI
# https://golangci-lint.run/usage/install/#local-installation
lint:
	golangci-lint run

# Registry documentation. tfplugindocs is pinned as a Go tool dependency (see the
# `tool` directive in go.mod), so these targets need no separately installed
# binary and CI runs exactly what contributors run. Generation reads the schema
# through the Terraform CLI: put a `terraform` binary on PATH, or tfplugindocs
# quietly downloads the latest release instead of the version you pinned.
docs:
	go tool tfplugindocs generate --provider-name ${NAME}

# docs-check regenerates and fails if the committed tree differs — the gate that
# stops templates, examples and published output drifting apart. Untracked files
# count too: a new resource whose page was never generated must not pass.
#
# It does not yet detect a changed schema Description, because the pages still
# hand-write their argument references instead of rendering {{ .SchemaMarkdown }};
# that migration is tracked in #256.
docs-check: docs
	@git diff --exit-code -- docs/ || { echo "::error::docs/ is stale — run 'make docs' and commit the result"; exit 1; }
	@test -z "$$(git ls-files --others --exclude-standard -- docs/)" || { \
		echo "::error::untracked generated docs:"; \
		git ls-files --others --exclude-standard -- docs/; exit 1; }
	go tool tfplugindocs validate --provider-name ${NAME}

# examples-validate type-checks every example against the provider built from
# this tree, via a dev_overrides CLI config. Overrides skip `terraform init`
# entirely, so validation needs no network and no credentials — and it checks the
# examples against THIS provider rather than whatever is published.
#
# Each .tf file is validated as its own module. Documentation snippets are
# alternatives to each other, not one composed configuration: two snippets on a
# page routinely declare the same resource name to show two ways of doing the
# same thing, which is a duplicate declaration if they share a module.
#
# The required_providers shim is injected into the throwaway module rather than
# into the examples, because published snippets should show the user's resource,
# not boilerplate. Without it Terraform resolves the bare name to
# hashicorp/namecheap and the dev override never matches.
examples-validate: build
	@set -e; \
	bin_dir="$$(pwd)"; \
	work="$$(mktemp -d -t namecheap-examples.XXXXXX)"; \
	trap 'rm -rf "$$work"' EXIT; \
	printf 'provider_installation {\n  dev_overrides {\n    "namecheap/namecheap" = "%s"\n  }\n  direct {}\n}\n' "$$bin_dir" > "$$work/tfrc"; \
	found=0; \
	find examples -name '*.tf' | sort > "$$work/list"; \
	while IFS= read -r tf; do \
		found=$$((found + 1)); \
		module="$$work/module"; \
		rm -rf "$$module"; mkdir -p "$$module"; \
		cp "$$tf" "$$module/main.tf"; \
		printf 'terraform {\n  required_providers {\n    namecheap = {\n      source = "namecheap/namecheap"\n    }\n  }\n}\n' > "$$module/versions.tf"; \
		echo "validating $$tf"; \
		TF_CLI_CONFIG_FILE="$$work/tfrc" terraform -chdir="$$module" validate >/dev/null \
			|| { echo "::error file=$$tf::terraform validate failed"; \
			     TF_CLI_CONFIG_FILE="$$work/tfrc" terraform -chdir="$$module" validate; exit 1; }; \
	done < "$$work/list"; \
	test "$$found" -gt 0 || { echo "::error::no examples found to validate"; exit 1; }; \
	echo "validated $$found example files"

.PHONY: format check test testacc testacc-sandbox testacc-mock build release install_darwin_amd64 install_linux_amd64 lint docs docs-check examples-validate
