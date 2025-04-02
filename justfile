build:
    go build .

db:
    pgcli postgresql://postgres:password@localhost:5555/mailstack

debug:
    dlv debug . -- server

gen-api:
    go run github.com/99designs/gqlgen generate --config ./api/graphql/gqlgen.yml

generate: gen-api

migrate:
    go run main.go migrate

run:
    go run main.go server

tidy:
    go mod tidy

trace:
    open "http://localhost:16686"

