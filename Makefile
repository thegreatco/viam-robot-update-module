PLATFORM=linux/arm64
PACKAGE_NAME=viam-config-update-module.tgz

VERSION_PATH=python/module.py
VERSION=$(shell grep 'VERSION' $(VERSION_PATH) | head -1 | sed -E 's/.*VERSION\s*=\s*"([^"]+)".*/\1/')
GIT_VERSION=$(shell git describe --tags --abbrev=0 | sed 's/^v//')

ifneq ($(VERSION),$(GIT_VERSION))
    $(warning VERSION ($(VERSION)) and GIT_VERSION ($(GIT_VERSION)) do not match)
endif

all: build

# Makefile
build-go:
	go mod -C go tidy
	go build -C go -o bin/viam-update-module module.go

build: clean
	mkdir -p ./python/dist
	cp ./python/init.sh python/dist/init.sh
	cp ./python/run.sh python/dist/run.sh
	python3 -m venv .venv
	./.venv/bin/pip3 install -r python/requirements.txt
	cd python; ../.venv/bin/python3 -m PyInstaller --onefile --hidden-import="googleapiclient" --add-binary="../.venv/lib/python3.11/site-packages/viam/rpc/libviam_rust_utils.so:viam/rpc/" module.py
	tar -czvf ${PACKAGE_NAME} meta.json -C python/dist module init.sh run.sh

clean:
	rm -rf ${PACKAGE_NAME}
	rm -rf go/bin
	rm -rf python/dist
	rm -rf python/build
	rm -rf python/module.spec

upload: build
	@if [ "$(VERSION)" != "$(GIT_VERSION)" ]; then \
		echo "VERSION ($(VERSION)) and GIT_VERSION ($(GIT_VERSION)) do not match"; \
		exit 1; \
	fi
	@if ! git diff-index --quiet HEAD --; then \
		echo "There are unstaged changes in tracked files. Please commit or stash them."; \
		exit 1; \
	fi
	@if [ "$$(git rev-parse HEAD)" != "$$(git rev-parse v$(GIT_VERSION))" ]; then \
		echo "HEAD is not the commit corresponding to the latest tag (v$(GIT_VERSION))."; \
		exit 1; \
	fi
	viam module update
	viam module upload --version=${VERSION} --platform=${PLATFORM} ${PACKAGE_NAME}
