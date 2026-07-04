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
testacc-mock:
	TF_ACC=1 TF_ACC_TERRAFORM_PATH="$${TF_ACC_TERRAFORM_PATH:-$$(command -v terraform)}" go test -tags testacc ./namecheap -v -run='^TestAccMock' -count=1 -timeout=10m

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

# Make sure you have installed https://github.com/hashicorp/terraform-plugin-docs
docs:
	tfplugindocs

.PHONY: format check test testacc testacc-sandbox testacc-mock build release install_darwin_amd64 install_linux_amd64 lint docs
