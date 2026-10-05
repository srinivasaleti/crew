PREFIX ?= $(HOME)/.local
BIN := crew

.PHONY: build test run install uninstall clean

build:
	go build -o $(BIN) .

test:
	go test ./...

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
