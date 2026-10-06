PREFIX ?= $(HOME)/.local
BIN := crew

.PHONY: setup build run install uninstall clean

# setup installs what crew needs.
setup:
	go mod download
	@command -v gh >/dev/null 2>&1 || brew install gh
	@gh auth status >/dev/null 2>&1 || echo "run: gh auth login   (the PR tab reads pull requests with it)"

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
