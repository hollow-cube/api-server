MODULES := $(shell find . -name "go.mod" -type f -print0 | xargs -0 -I {} dirname {} )
MAKEFILE_DIR := $(shell pwd)

.ONESHELL: # Add this directive to execute the recipe in a single shell

#gen:
#	cd ./services/backend/api/v1 && ./openapi-go public.openapi.yaml
#	cd "$(MAKEFILE_DIR)";

lint:
	@for module in $(MODULES); do \
		echo "Linting module: $$module"; \
		cd "$(MAKEFILE_DIR)"; \
		cd "$$module" && golangci-lint run ./...; \
	done

tidy:
	@for module in $(MODULES); do \
		echo "Tidying module: $$module"; \
		cd "$(MAKEFILE_DIR)";
		cd "$$module" && go mod tidy; \
	done
