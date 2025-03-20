
build:
    go build .

db:
    pgcli postgresql://postgres:password@localhost:5555/mailstack

debug:
    dlv debug . -- server

gen-api:
    go run github.com/99designs/gqlgen generate --config ./api/graphql/gqlgen.yml

migrate:
    go run main.go migrate

rabbit:
    open "http://localhost:15672"

run:
    go run main.go server

tidy:
    go mod tidy

trace:
    open "http://localhost:16686"

