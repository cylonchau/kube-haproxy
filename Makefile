.PHONY: all build run gotool clean help
.DEFAULT_GOAL := help
BINARY_NAME=kube-haproxy
GOCMD=go
GOBUILD=$(GOCMD) build
GOBUILD_DIR=cmd
OUT_DIR ?= _output
BIN_DIR := $(OUT_DIR)/bin

dicCheck = $(shell if [ -d $(OUT_DIR) ]; then echo "true"; else echo "false"; fi)

build:
	hack/build.sh $(BINARY_NAME)

clean:
    ifneq ("$(dicCheck)" "true",)
    	$(shell rm -fr $(OUT_DIR))
    endif

help:
	@ echo "all build run gotool clean help"
	@ echo ""
	@ echo "Example:"
	@ echo "	make build"
	@ echo "	make clean"
	@ echo "	make help"
	@ echo "	make rpm"