# Makefile
build-go:
	go mod -C go tidy
	go build -C go -o bin/viam-update-module module.go

build-python:
	mkdir -p ./python/dist
	python3 -m venv .venv
	cp ./python/init.sh python/dist/init.sh
	cp ./python/run.sh python/dist/run.sh
	./.venv/bin/pip3 install -r python/requirements.txt
	cd python; ../.venv/bin/python3 -m PyInstaller --onedir --hidden-import="googleapiclient" --add-binary="../.venv/lib/python3.11/site-packages/viam/rpc/libviam_rust_utils.so:viam/rpc/" module.py
	cd python; tar -czvf dist/archive.tar.gz dist/module dist/init.sh dist/run.sh

clean:
	rm -rf go/bin
	rm -rf python/dist
	rm -rf python/build
	rm -rf python/module.spec
