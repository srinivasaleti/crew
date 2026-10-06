PREFIX ?= $(HOME)/.local
BIN := crew

.PHONY: setup build run install uninstall clean

# setup installs what crew needs: Go modules and micro, the editor in the Files tab.
setup:
	go mod download
	@command -v micro >/dev/null 2>&1 || { command -v brew >/dev/null 2>&1 && brew install micro || echo "install micro from https://micro-editor.github.io"; }

build:
	go build -o $(BIN) .

run: build
	./$(BIN)

install: build
	install -d $(PREFIX)/bin
	install -m 755 $(BIN) $(PREFIX)/bin/$(BIN)
	@echo "installed $(PREFIX)/bin/$(BIN)"

uninstall:
	rm -f $(PREFIX)/bin/$(BIN)

clean:
	rm -f $(BIN)
