REGISTRY := ghcr.io/banksean/sand

.PHONY: all base claude codex default opencode push push-base push-claude push-codex push-default push-opencode

all: base claude codex default opencode

base:
	container build -t $(REGISTRY)/base:latest base/

claude: base
	container build -t $(REGISTRY)/claude:latest claude/

codex: base
	container build -t $(REGISTRY)/codex:latest codex/

default: base
	container build -t $(REGISTRY)/default:latest default/

opencode:
	container build -t $(REGISTRY)/opencode:latest opencode/

push: push-base push-claude push-codex push-default push-opencode

push-base:
	container push $(REGISTRY)/base:latest

push-claude:
	container push $(REGISTRY)/claude:latest

push-codex:
	container push $(REGISTRY)/codex:latest

push-default:
	container push $(REGISTRY)/default:latest

push-opencode:
	container push $(REGISTRY)/opencode:latest
