PREFIX ?= $(HOME)/.local
BIN := crew

.PHONY: build run install uninstall clean

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
