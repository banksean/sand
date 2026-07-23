REGISTRY := ghcr.io/banksean/sand
BUILD_CMD := container build
PUSH_CMD := container image push
SAND_CLI_VERSION ?= v0.1.0
SAND_CLI_BUILD_ARG := --build-arg SAND_CLI_VERSION=$(SAND_CLI_VERSION)

.PHONY: all base push push-base sandless ubuntu-sand bookworm

all: base

base:
	$(BUILD_CMD) $(SAND_CLI_BUILD_ARG) -t $(REGISTRY)/base:latest -t base:local base/

push: push-base

push-base:
	$(PUSH_CMD) $(REGISTRY)/base:latest

sandless:
	$(BUILD_CMD) sandless sandless/

ubuntu-sand:
	$(BUILD_CMD) $(SAND_CLI_BUILD_ARG) -t ubuntu-sand ubuntu-sand/

