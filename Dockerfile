# ---- build stage ----
FROM golang:1.22-alpine AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
# CGO is disabled: modernc.org/sqlite is pure Go, so the binary is fully static.
RUN CGO_ENABLED=0 go build -o /out/sudoku-bot ./cmd/bot

# ---- run stage ----
FROM alpine:3.20
RUN apk add --no-cache tzdata ca-certificates
WORKDIR /app
COPY --from=build /out/sudoku-bot /app/sudoku-bot

# SQLite file lives on a mounted volume so data survives restarts.
ENV DB_PATH=/data/sudoku.db
VOLUME ["/data"]

ENTRYPOINT ["/app/sudoku-bot"]
