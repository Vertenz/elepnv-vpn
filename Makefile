.PHONY: help dev app-typecheck app-package clean

help:
	@echo "Targets:"
	@echo "  make dev            Start the Electron app"
	@echo "  make app-typecheck  Type-check the Electron app"
	@echo "  make app-package    Package the Electron app"
	@echo "  make clean          Remove local build artifacts"

dev:
	@./scripts/dev.sh

app-typecheck:
	cd app && npm run typecheck

app-package:
	cd app && npm run package

clean:
	rm -rf dist app/out app/.vite daemon/bin run
