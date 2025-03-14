
build:
    go build .

db:
    pgcli postgresql://postgres:password@localhost:5555/mailstack

migrate:
    go run main.go migrate

run:
    go run main.go server

tidy:
    go mod tidy

start:
	cd ./deployments && podman-compose up -d

stop:
	cd ./deployments && podman-compose down
