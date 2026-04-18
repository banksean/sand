REGISTRY := ghcr.io/banksean/sand
BUILD_CMD := container build -t
PUSH_CMD := container image push

.PHONY: all base claude codex default opencode push push-base push-claude push-codex push-default push-opencode

all: base claude codex default opencode

base:
	$(BUILD_CMD) $(REGISTRY)/base:latest base/

claude: base
	$(BUILD_CMD) $(REGISTRY)/claude:latest claude/

codex: base
	$(BUILD_CMD) $(REGISTRY)/codex:latest codex/

default: base
	$(BUILD_CMD) $(REGISTRY)/default:latest default/

gemini: base
	$(BUILD_CMD) $(REGISTRY)/gemini:latest gemini/

opencode:
	$(BUILD_CMD) $(REGISTRY)/opencode:latest opencode/

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
