PREFIX ?= $(HOME)/.local
LIBDIR := $(PREFIX)/lib/music-bridge
BINDIR := $(PREFIX)/bin
DISTDIR := dist/music-bridge

.PHONY: all build install uninstall clean

all: install

build:
	rm -rf "$(DISTDIR)"
	mkdir -p "$(DISTDIR)"
	go build -o "$(DISTDIR)/music-bridge" ./cmd/music-bridge
	cp -R scripts "$(DISTDIR)/scripts"

install: build
	mkdir -p "$(PREFIX)/lib" "$(BINDIR)"
	rm -rf "$(LIBDIR)"
	cp -R "$(DISTDIR)" "$(LIBDIR)"
	ln -sfn "../lib/music-bridge/music-bridge" "$(BINDIR)/music-bridge"

uninstall:
	rm -f "$(BINDIR)/music-bridge"
	rm -rf "$(LIBDIR)"

clean:
	rm -rf dist
