.PHONY: build test run docker-up docker-down migrate clean

build:
	go build -o devmemory ./cmd/devmemory

test:
	go test ./... -v -count=1 -race

run: build
	./devmemory --migrate

docker-up:
	docker compose up -d

docker-down:
	docker compose down

migrate: build
	./devmemory --migrate --exit-after-migrate

clean:
	rm -f devmemory
	docker compose down -v

lint:
	go vet ./...
