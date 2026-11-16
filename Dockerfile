# ---- build stage ----
# go.mod requires Go >= 1.25 (modernc.org/sqlite); match it here.
FROM golang:1.25-alpine AS build
WORKDIR /src

COPY go.mod go.sum ./
# go.mod replaces telebot with the patched copy in third_party (see its PATCHES.md).
COPY third_party ./third_party
RUN go mod download

COPY . .
# CGO is disabled: modernc.org/sqlite is pure Go, so the binary is fully static.
# COMMIT is logged at start ("build <commit>"); deploy/autodeploy.sh passes the
# commit it deploys and then looks for it in the logs.
ARG COMMIT=dev
RUN CGO_ENABLED=0 go build -ldflags "-X main.commit=${COMMIT}" -o /out/sudoku-bot ./cmd/bot

# ---- run stage ----
FROM alpine:3.20
RUN apk add --no-cache tzdata ca-certificates
WORKDIR /app
COPY --from=build /out/sudoku-bot /app/sudoku-bot

# SQLite file lives on a mounted volume so data survives restarts.
ENV DB_PATH=/data/sudoku.db
VOLUME ["/data"]

ENTRYPOINT ["/app/sudoku-bot"]
