PROJECT_NAME := $(notdir $(CURDIR))
BINARY_NAME := $(PROJECT_NAME)

PROJECT_ROOT := $(CURDIR)
BACKEND_SOURCE_DIR := $(PROJECT_ROOT)/app
FRONTEND_SOURCE_DIR := $(PROJECT_ROOT)/web/prod

BUILD_DIR := $(PROJECT_ROOT)/build
CURRENT_BUILD_DIR := $(BUILD_DIR)/Goptivum
DIST_DIR := $(CURRENT_BUILD_DIR)/dist

DEPLOY_HOST ?= razul-server.local
DEPLOY_USER ?= goptivum
DEPLOY_DIR ?= ~/$(PROJECT_NAME)
DEPLOY_SERVICE_NAME ?= goptivum.service

VERSION := $(shell grep '^VERSION=' $(PROJECT_ROOT)/.env | cut -d '=' -f 2)
ARCHS := amd64 arm
OS := linux

DOCKER_IMAGE_NAME := $(shell echo $(PROJECT_NAME) | tr '[:upper:]' '[:lower:]'):$(VERSION)
DOCKER_COMPOSE := docker-compose
DOCKER_COMPOSE_FILE := $(PROJECT_ROOT)/docker-compose.yml

DOCKERHUB_USERNAME ?= smeggmann99

.PHONY: all
all: build build-web-install

.PHONY: build
build: | $(CURRENT_BUILD_DIR)
	@echo "Building backend..."
	@CGO_ENABLED=0 GOOS=$(OS) GOARCH=amd64 go build -a -installsuffix cgo -o $(CURRENT_BUILD_DIR)/$(BINARY_NAME) $(BACKEND_SOURCE_DIR)
	@cp -r $(BACKEND_SOURCE_DIR)/config.json $(CURRENT_BUILD_DIR)/config.json
	@cp -r $(BACKEND_SOURCE_DIR)/.env $(CURRENT_BUILD_DIR)/.env

.PHONY: build-web
build-web: | $(CURRENT_BUILD_DIR)
	@echo "Building frontend..."
	@cd $(FRONTEND_SOURCE_DIR) && npm run build
	@mkdir -p $(DIST_DIR)
	@cp -r $(FRONTEND_SOURCE_DIR)/dist/* $(DIST_DIR)

.PHONY: build-web-install
build-web-install: | $(CURRENT_BUILD_DIR)
	@echo "Building frontend and installing dependencies..."
	@cd $(FRONTEND_SOURCE_DIR) && npm install && npm run build
	@mkdir -p $(DIST_DIR)
	@cp -r $(FRONTEND_SOURCE_DIR)/dist/* $(DIST_DIR)

.PHONY: package
package: all
	@echo "Packaging tarballs for version $(VERSION)..."
	@for arch in $(ARCHS); do \
		BUILD_ARCHIVE=$(BUILD_DIR)/$(PROJECT_NAME)-$(VERSION)-$(OS)-static-$$arch.tar.gz; \
		echo "Building for $$arch..."; \
		TEMP_DIR=$(BUILD_DIR)/temp_$$arch; \
		mkdir -p $$TEMP_DIR/Goptivum; \
		GOOS=$(OS) GOARCH=$$arch CGO_ENABLED=0 go build -a -installsuffix cgo -o $$TEMP_DIR/Goptivum/$(BINARY_NAME) $(BACKEND_SOURCE_DIR); \
		cp -r $(CURRENT_BUILD_DIR)/config.json $$TEMP_DIR/Goptivum/config.json; \
		cp -r $(CURRENT_BUILD_DIR)/.env $$TEMP_DIR/Goptivum/.env; \
		cp -r $(DIST_DIR) $$TEMP_DIR/Goptivum/dist; \
		echo "Creating archive $$BUILD_ARCHIVE..."; \
		tar -czvf $$BUILD_ARCHIVE -C $$TEMP_DIR Goptivum; \
		rm -rf $$TEMP_DIR; \
	done
	@echo "Packaging complete. Tarballs are in $(BUILD_DIR)"

.PHONY: docker-build
docker-build:
	@echo "Building Docker images with version $(VERSION)..."
	@VERSION=$(VERSION) $(DOCKER_COMPOSE) -f $(DOCKER_COMPOSE_FILE) build

.PHONY: docker-up
docker-up:
	@echo "Starting Docker containers..."
	@$(DOCKER_COMPOSE) -f $(DOCKER_COMPOSE_FILE) up -d

.PHONY: docker-down
docker-down:
	@echo "Stopping Docker containers..."
	@$(DOCKER_COMPOSE) -f $(DOCKER_COMPOSE_FILE) down

.PHONY: docker-clean
docker-clean:
	@echo "Removing Docker containers, images, and volumes..."
	@$(DOCKER_COMPOSE) -f $(DOCKER_COMPOSE_FILE) down --rmi all --volumes --remove-orphans

.PHONY: docker-publish
docker-publish: docker-build
	@echo "Tagging Docker image for publishing..."
	@docker tag goptivum:$(VERSION) $(DOCKERHUB_USERNAME)/goptivum:$(VERSION)
	@docker tag goptivum:$(VERSION) $(DOCKERHUB_USERNAME)/goptivum:latest
	@echo "Pushing Docker image to Docker Hub..."
	@docker push $(DOCKERHUB_USERNAME)/goptivum:$(VERSION)
	@docker push $(DOCKERHUB_USERNAME)/goptivum:latest

.PHONY: clean
clean:
	@echo "Cleaning up build files..."
	@rm -rf $(BUILD_DIR)

.PHONY: clean-tarballs
clean-tarballs:
	@echo "Cleaning up tarballs..."
	@find $(BUILD_DIR) -type f -name '*.tar.gz' -delete

.PHONY: deploy
deploy: all
	@echo "Deploying to $(DEPLOY_HOST)..."
	@rsync -avz --delete $(BUILD_DIR)/ $(DEPLOY_USER)@$(DEPLOY_HOST):$(DEPLOY_DIR)
	@ssh -t $(DEPLOY_USER)@$(DEPLOY_HOST) 'sudo systemctl restart $(DEPLOY_SERVICE_NAME) && sudo systemctl reload nginx'
	@echo "Deployment complete."

.PHONY: help
help:
	@echo "Usage:"
	@echo "  make                      Build the Go backend and Vue.js frontend and install NPM dependencies"
	@echo "  make build                Build the Go backend"
	@echo "  make build-web            Build the Vue.js frontend without installing NPM dependencies"
	@echo "  make build-web-install    Build the Vue.js frontend and install NPM dependencies"
	@echo "  make package              Build and package the application for all architectures"
	@echo "  make docker-build         Build Docker images"
	@echo "  make docker-up            Start Docker containers"
	@echo "  make docker-down          Stop Docker containers"
	@echo "  make docker-clean         Clean up Docker containers, images, and volumes"
	@echo "  make docker-publish       Build and publish Docker images to Docker Hub"
	@echo "  make deploy               Deploy the application to the remote server"
	@echo "  make clean                Clean up build files"
	@echo "  make clean-tarballs       Clean up tarball archives"
	@echo "  make help                 Show this help message"

$(CURRENT_BUILD_DIR):
	@mkdir -p $(CURRENT_BUILD_DIR)
