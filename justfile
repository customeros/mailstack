
build:
    go build .

migrate:
    go run main.go migrate

run:
    go run main.go server

tidy:
    go mod tidy
