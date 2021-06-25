FROM golang:1.15

COPY . /go/src/screeps-exporter/
RUN go get screeps-exporter/...
RUN go install screeps-exporter

CMD screeps-exporter
