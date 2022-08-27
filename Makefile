docker-compose-up:
	docker-compose -f docker-compose-db.yaml -f docker-compose.yaml up -d

docker-compose-up-local:
	docker-compose -f docker-compose-db-local.yaml -f docker-compose.yaml up -d

docker-compose-up-build:
	docker-compose -f docker-compose-db.yaml -f docker-compose.yaml up -d --build

docker-compose-up-build-local:
	docker-compose -f docker-compose-db-local.yaml -f docker-compose.yaml up -d --build

docker-compose-down:
	docker-compose -f docker-compose-db.yaml -f docker-compose.yaml down

.PHONY: docker-compose-up docker-compose-up-build docker-compose-down docker-compose-up-build-local docker-compose-up-local