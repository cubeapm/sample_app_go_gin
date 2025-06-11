FROM golang:1.23.3-alpine3.20

WORKDIR /app

ADD go.mod .
ADD go.sum .
RUN go mod download -x

ADD . .

RUN go build -o ./main .

CMD ["./main"]
