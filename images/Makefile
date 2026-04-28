REGISTRY := ghcr.io/banksean/sand
BUILD_CMD := container build -t
PUSH_CMD := container image push
SAND_CLI_VERSION ?= v0.0.10
SAND_CLI_BUILD_ARG := --build-arg SAND_CLI_VERSION=$(SAND_CLI_VERSION)

.PHONY: all base claude codex default opencode push push-base push-claude push-codex push-default push-opencode sandless ubuntu-sand

all: claude codex default opencode

base:
	$(BUILD_CMD) $(SAND_CLI_BUILD_ARG) $(REGISTRY)/base:latest -t base:local base/

claude: base
	$(BUILD_CMD) $(REGISTRY)/claude:latest -t claude:local claude/

codex: base
	$(BUILD_CMD) $(REGISTRY)/codex:latest -t codex:local codex/

default: base
	$(BUILD_CMD) $(REGISTRY)/default:latest -t default:local default/

gemini: base
	$(BUILD_CMD) $(REGISTRY)/gemini:latest -t gemini:local gemini/

opencode: base
	$(BUILD_CMD) $(REGISTRY)/opencode:latest -t opencode:local opencode/

push: push-base push-claude push-codex push-default push-opencode

push-base:
	$(PUSH_CMD) $(REGISTRY)/base:latest

push-claude:
	$(PUSH_CMD) $(REGISTRY)/claude:latest

push-codex:
	$(PUSH_CMD) $(REGISTRY)/codex:latest

push-default:
	$(PUSH_CMD) $(REGISTRY)/default:latest

push-opencode:
	$(PUSH_CMD) $(REGISTRY)/opencode:latest

sandless:
	$(BUILD_CMD) sandless sandless/

ubuntu-sand:
	$(BUILD_CMD) $(SAND_CLI_BUILD_ARG) ubuntu-sand ubuntu-sand/
