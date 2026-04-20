REGISTRY := ghcr.io/banksean/sand
BUILD_CMD := container build -t
PUSH_CMD := container image push

.PHONY: all base claude codex default opencode push push-base push-claude push-codex push-default push-opencode

all: base claude codex default opencode

base:
	$(BUILD_CMD) $(REGISTRY)/base:latest -t $(REGISTRY)/base:local base/

claude: base
	$(BUILD_CMD) $(REGISTRY)/claude:latest -t $(REGISTRY)/claude:local claude/

codex: base
	$(BUILD_CMD) $(REGISTRY)/codex:latest -t $(REGISTRY)/codex:local codex/

default: base
	$(BUILD_CMD) $(REGISTRY)/default:latest -t $(REGISTRY)/default:local default/

gemini: base
	$(BUILD_CMD) $(REGISTRY)/gemini:latest -t $(REGISTRY)/gemini:local gemini/

opencode:
	$(BUILD_CMD) $(REGISTRY)/opencode:latest -t $(REGISTRY)/opencode:local opencode/

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
