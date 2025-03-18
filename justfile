
build:
    go build .

db:
    pgcli postgresql://postgres:password@localhost:5555/mailstack

debug:
    dlv debug . -- server

down:
	cd ./deployments && podman-compose down

gen-api:
    go run github.com/99designs/gqlgen generate --config ./api/graphql/gqlgen.yml

migrate:
    go run main.go migrate

run:
    go run main.go server

tidy:
    go mod tidy

up:
	cd ./deployments && podman-compose up -d
