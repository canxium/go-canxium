# Makefile for building the kawpow native library

KAWPOW_SRC_DIR := $(CURDIR)/src
KAWPOW_BUILD_DIR := $(KAWPOW_SRC_DIR)/build
# Allow override of output directory
OUTPUT_DIR ?= $(CURDIR)/lib
KAWPOW_LIB := $(OUTPUT_DIR)/libkawpow.a

.PHONY: all $(KAWPOW_LIB)

all: $(KAWPOW_LIB)

$(KAWPOW_LIB):
	rm -rf $(KAWPOW_BUILD_DIR)
	mkdir -p $(KAWPOW_BUILD_DIR)
	mkdir -p $(OUTPUT_DIR)
	cd $(KAWPOW_BUILD_DIR) && cmake .. && make
	cp $(KAWPOW_BUILD_DIR)/lib/libkawpow.a $(OUTPUT_DIR)/
	rm -rf $(KAWPOW_BUILD_DIR)

clean:
	rm -rf $(KAWPOW_BUILD_DIR)
