INIT_IMAGE_NAME := ghcr.io/banksean/sand/custom-init

initwrapper:
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o ./image/initwrapper ./cmd/initwrapper

image: initwrapper
	container build -t $(INIT_IMAGE_NAME):latest ./image