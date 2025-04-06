FROM golang:1.24.1-alpine

WORKDIR /app

COPY go.mod go.sum ./

RUN go mod download

COPY . ./

RUN go build -ldflags "-w -s" -o main ./cmd/web

EXPOSE 3000

ENTRYPOINT ["/app/main"]
