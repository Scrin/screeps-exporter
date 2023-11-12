FROM golang:1.20

WORKDIR /go/src/github.com/Scrin/screeps-exporter/
COPY . ./
RUN go install .

CMD screeps-exporter
