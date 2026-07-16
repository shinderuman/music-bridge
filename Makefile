PREFIX ?= $(HOME)/.local
LIBDIR := $(PREFIX)/lib/music-bridge
BINDIR := $(PREFIX)/bin

.PHONY: build install uninstall

build:
	go build -o music-bridge ./alpha/go/cmd/music-bridge

install:
	mkdir -p "$(LIBDIR)" "$(BINDIR)"
	go build -o "$(LIBDIR)/music-bridge" ./alpha/go/cmd/music-bridge
	rm -rf "$(LIBDIR)/scripts"
	cp -R scripts "$(LIBDIR)/scripts"
	ln -sfn "../lib/music-bridge/music-bridge" "$(BINDIR)/music-bridge"

uninstall:
	rm -f "$(BINDIR)/music-bridge"
	rm -rf "$(LIBDIR)"
