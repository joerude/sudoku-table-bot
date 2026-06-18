# Sudoku League bot — task runner. Run `just` to see all commands.
# Recipes load variables from .env automatically (BOT_TOKEN, etc).
set dotenv-load := true

# Show all available commands
default:
    @just --list

# --- Local development ---

# Run the bot locally (uses .env; SQLite DB in ./data)
run:
    DB_PATH=./data/sudoku.db go run ./cmd/bot

# Build all packages
build:
    go build ./...

# Run unit + integration tests
test:
    go test ./...

# Vet for suspicious constructs
vet:
    go vet ./...

# Format the code
fmt:
    go fmt ./...

# Tidy go.mod / go.sum
tidy:
    go mod tidy

# Build, vet and test in one go (pre-commit check)
check: build vet test

# --- Docker (deployment) ---

# Build the image and start the bot detached
up:
    docker compose up -d --build

# Stop and remove the container
down:
    docker compose down

# Clean rebuild (no cache) and recreate the container
rebuild:
    docker compose build --no-cache
    docker compose up -d --force-recreate

# Restart the running container (no rebuild)
restart:
    docker compose restart bot

# Follow live logs
logs:
    docker compose logs -f bot

# Show container status
ps:
    docker compose ps
