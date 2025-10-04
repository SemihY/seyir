BINARY=logspot

build:
	go build -o $(BINARY) cmd/logspot/main.go

run:
	go run cmd/logspot/main.go

docker-build:
	docker build -t $(BINARY) .

docker-up:
	docker-compose up --build

release:
	goreleaser release --rm-dist

brew:
	# Brew tap için formül oluştur
	goreleaser release --rm-dist --snapshot
