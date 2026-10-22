BIN := crew
APP := crew.app
APP_DIR ?= /Applications

.PHONY: setup doctor acli build app run install-app uninstall-app clean

# setup installs what crew needs.
setup: acli
	go mod download
	@xcode-select -p >/dev/null 2>&1 || xcode-select --install # Apple's command line tools, to build the app
	@command -v gh >/dev/null 2>&1 || brew install gh
	@command -v claude >/dev/null 2>&1 || echo "install Claude Code: curl -fsSL https://claude.ai/install.sh | bash"
	@go run . doctor || echo "crew walks you through what's missing when you open it (or run make doctor again)"

# acli installs Atlassian's CLI, which JIRA reads your issues with, the way Atlassian documents it with
# curl, into ~/.local/bin (where Claude Code installs too, so no sudo). setup.AcliInstall is the same
# command for crew's Setup panel. Signing in happens in crew (or: acli jira auth login --web).
acli:
	@command -v acli >/dev/null 2>&1 || { mkdir -p ~/.local/bin && \
		curl -fsSL -o ~/.local/bin/acli "https://acli.atlassian.com/darwin/latest/acli_darwin_$$(uname -m | sed s/x86_64/amd64/)/acli" && \
		chmod +x ~/.local/bin/acli && echo "installed acli in ~/.local/bin"; }

# doctor checks that Claude Code, the GitHub CLI and git are installed and signed in.
doctor:
	@go run . doctor

# build makes the crew binary, which the app runs. It reads crew's skills (plugin/) from this
# checkout, so editing a skill needs no rebuild; the app also carries a copy, used when the
# checkout isn't there.
build:
	go build -ldflags "-X main.pluginDir=$(CURDIR)/plugin" -o $(BIN) .

# app builds crew.app: a window (desktop/) running the crew binary built just above. desktop/ holds
# no crew features; they all live in the crew binary.
app: build
	CGO_LDFLAGS="-framework UniformTypeIdentifiers" CGO_CFLAGS="-Wno-deprecated-declarations" \
		go build -tags desktop,production -ldflags "-w -s" -o crew-desktop ./desktop
	rm -rf $(APP)
	mkdir -p $(APP)/Contents/MacOS $(APP)/Contents/Resources
	cp desktop/Info.plist $(APP)/Contents/
	cp -R plugin $(APP)/Contents/Resources/plugin # crew's skills, for when this checkout isn't there
	mv crew-desktop $(APP)/Contents/MacOS/
	cp $(BIN) $(APP)/Contents/MacOS/crew
	codesign --force --deep --sign - $(APP)
	@echo "built $(APP)"

# run builds the app and opens it from here, without installing it.
run: app
	open $(APP)

install-app: app acli
	rm -rf $(APP_DIR)/$(APP)
	cp -R $(APP) $(APP_DIR)/
	@echo "installed $(APP_DIR)/$(APP)"

uninstall-app:
	rm -rf $(APP_DIR)/$(APP)
	@echo "removed $(APP_DIR)/$(APP)"

clean:
	rm -rf $(BIN) $(APP)
