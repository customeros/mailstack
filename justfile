help:
    @echo "Available commands:"
    @echo "  help        - Show this help message"
    @echo "  build       - Build the application"
    @echo "  db          - Connect to PostgreSQL database using pgcli"
    @echo "  debug       - Start application in debug mode using Delve"
    @echo "  gen-api     - Generate GraphQL code using gqlgen"
    @echo "  generate    - Run all code generation tasks"
    @echo "  gen-proto   - Generate Protocol Buffers code"
    @echo "  migrate     - Run database migrations"
    @echo "  run         - Run the application server"
    @echo "  tidy        - Tidy and verify Go module dependencies"

build:
    go build .

db:
    pgcli postgresql://postgres:password@localhost:5555/mailstack

debug:
    dlv debug . -- server

gen-api:
    go run github.com/99designs/gqlgen generate --config ./api/graphql/gqlgen.yml

generate: gen-api

gen-proto:
    find ./proto -name "*.proto" -type f -exec \
    protoc \
    --proto_path=./proto \
    --go_out=./proto/pb \
    --go_opt=module=github.com/customeros/mailstack/proto/pb \
    --go-grpc_out=./proto/pb \
    --go-grpc_opt=module=github.com/customeros/mailstack/proto/pb \
    {} \;

migrate:
    go run main.go migrate

run:
    go run main.go server

tidy:
    go mod tidy

