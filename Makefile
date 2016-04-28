TEST?=./...
NAME?=$(shell basename "$(CURDIR)")
VERSION = $(shell awk -F\" '/^const Version/ { print $$2; exit }' main.go)

default: test

# bin generates the releasable binaries
bin:
	@sh -c "'$(CURDIR)/scripts/build.sh'"

# dev creates binaries for testing locally - they are put into ./bin and $GOPATH
dev:
	@DEV=1 sh -c "'$(CURDIR)/scripts/build.sh'"

# dist creates the binaries for distibution
dist:
	@sh -c "'$(CURDIR)/scripts/dist.sh' $(VERSION)"

# integration runs the integration tests
integration: generate
	@sh -c "'$(CURDIR)/scripts/integration.sh'"

# test runs the test suite and vets the code
test: generate
	go list $(TEST) | grep -v ^github.com/hashicorp/consul-replicate/vendor/ | xargs -n1 go test $(TESTARGS) -timeout=60s -parallel=10

# testrace runs the race checker
testrace: generate
	go list $(TEST) | xargs -n1 go test $(TEST) $(TESTARGS) -race

# updatedeps installs all the dependencies needed to run and build - this is
# specifically designed to only pull deps, but not self.
updatedeps:
	go get -u github.com/tools/godep
	go get -u github.com/mitchellh/gox
	go list ./... \
		| xargs go list -f '{{ join .Deps "\n" }}{{ printf "\n" }}{{ join .TestImports "\n" }}' \
		| grep -v github.com/hashicorp/$(NAME) \
		| xargs go get -f -u -v

.PHONY: default bin dev dist integration test testrace updatedeps generate
